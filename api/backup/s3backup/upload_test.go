package s3backup

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/stretchr/testify/require"
)

type testCredentialsProvider struct {
	credentials aws.Credentials
	providerErr error
}

func (p testCredentialsProvider) Retrieve(context.Context) (aws.Credentials, error) {
	return p.credentials, p.providerErr
}

func newRoleCredentials() aws.Credentials {
	return aws.Credentials{
		AccessKeyID:     "temporary-access-key",
		SecretAccessKey: "temporary-secret-key",
		SessionToken:    "temporary-session-token",
		CanExpire:       true,
		Expires:         time.Now().UTC().Add(time.Hour),
	}
}

func TestRoleCredentialsRequireTemporarySessionCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		credentials aws.Credentials
		wantErr     string
	}{
		{
			name:        "temporary session credentials",
			credentials: newRoleCredentials(),
		},
		{
			name: "static credentials",
			credentials: aws.Credentials{
				AccessKeyID:     "static-access-key",
				SecretAccessKey: "static-secret-key",
			},
			wantErr: "unexpired temporary session credentials",
		},
		{
			name: "temporary credentials without session token",
			credentials: aws.Credentials{
				AccessKeyID:     "temporary-access-key",
				SecretAccessKey: "temporary-secret-key",
				CanExpire:       true,
				Expires:         time.Now().UTC().Add(time.Hour),
			},
			wantErr: "unexpired temporary session credentials",
		},
		{
			name: "expired session credentials",
			credentials: aws.Credentials{
				AccessKeyID:     "temporary-access-key",
				SecretAccessKey: "temporary-secret-key",
				SessionToken:    "temporary-session-token",
				CanExpire:       true,
				Expires:         time.Now().UTC().Add(-time.Minute),
			},
			wantErr: "unexpired temporary session credentials",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := roleCredentials{provider: testCredentialsProvider{credentials: test.credentials}}
			credentials, err := provider.Retrieve(context.Background())
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				require.Empty(t, credentials.AccessKeyID)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.credentials.AccessKeyID, credentials.AccessKeyID)
		})
	}
}

func resetAWSConfigEnvironment(t *testing.T) string {
	t.Helper()
	for _, name := range []string{
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN",
		"AWS_WEB_IDENTITY_TOKEN_FILE",
		"AWS_ROLE_ARN",
		"AWS_ROLE_SESSION_NAME",
		"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI",
		"AWS_CONTAINER_CREDENTIALS_FULL_URI",
		"AWS_CONTAINER_AUTHORIZATION_TOKEN",
		"AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE",
		"AWS_PROFILE",
		"AWS_DEFAULT_PROFILE",
		"AWS_REGION",
		"AWS_DEFAULT_REGION",
		"AWS_CONFIG_FILE",
		"AWS_SHARED_CREDENTIALS_FILE",
		"AWS_ENDPOINT_URL",
		"AWS_ENDPOINT_URL_S3",
		"AWS_EC2_METADATA_SERVICE_ENDPOINT",
		"AWS_EC2_METADATA_SERVICE_ENDPOINT_MODE",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	awsFiles := t.TempDir()
	configPath := filepath.Join(awsFiles, "config")
	credentialsPath := filepath.Join(awsFiles, "credentials")
	require.NoError(t, os.WriteFile(configPath, nil, 0600))
	require.NoError(t, os.WriteFile(credentialsPath, nil, 0600))
	t.Setenv("AWS_CONFIG_FILE", configPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentialsPath)
	return credentialsPath
}

func TestCheckRoleSourcesRejectsStaticEnvironmentCredentials(t *testing.T) {
	resetAWSConfigEnvironment(t)
	t.Setenv("AWS_ACCESS_KEY_ID", "static-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "static-secret-key")

	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion("us-east-1"))
	require.NoError(t, err)
	require.ErrorContains(t, checkRoleSources(cfg.Credentials), "static credentials are not supported")
}

func TestCheckRoleSourcesRejectsStaticSharedProfileCredentials(t *testing.T) {
	credentialsPath := resetAWSConfigEnvironment(t)
	require.NoError(t, os.WriteFile(credentialsPath, []byte("[default]\naws_access_key_id = static-access-key\naws_secret_access_key = static-secret-key\n"), 0600))
	t.Setenv("AWS_PROFILE", "default")

	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion("us-east-1"))
	require.NoError(t, err)
	require.ErrorContains(t, checkRoleSources(cfg.Credentials), "static credentials are not supported")
}

func TestCheckRoleSourcesAcceptsTemporaryECSRoleCredentials(t *testing.T) {
	resetAWSConfigEnvironment(t)
	credentials := newRoleCredentials()
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"AccessKeyId":"` + credentials.AccessKeyID + `","SecretAccessKey":"` + credentials.SecretAccessKey + `","Token":"` + credentials.SessionToken + `","Expiration":"` + credentials.Expires.Format(time.RFC3339) + `"}`))
	}))
	defer endpoint.Close()
	t.Setenv("AWS_CONTAINER_CREDENTIALS_FULL_URI", endpoint.URL)

	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion("us-east-1"))
	require.NoError(t, err)
	require.NoError(t, checkRoleSources(cfg.Credentials))

	resolved, err := (roleCredentials{provider: cfg.Credentials}).Retrieve(context.Background())
	require.NoError(t, err)
	require.Equal(t, credentials.AccessKeyID, resolved.AccessKeyID)
	require.Equal(t, credentials.SessionToken, resolved.SessionToken)
}

func TestCheckRoleSourcesAcceptsTemporaryEC2InstanceRoleCredentials(t *testing.T) {
	resetAWSConfigEnvironment(t)
	credentials := newRoleCredentials()
	requestCount := 0
	var requestPaths []string
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		requestPaths = append(requestPaths, r.Method+" "+r.URL.Path+" token="+r.Header.Get("X-aws-ec2-metadata-token"))
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/latest/api/token":
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("X-Aws-Ec2-Metadata-Token-Ttl-Seconds", "300")
			_, _ = w.Write([]byte("fake-imdsv2-token"))
		case r.Method == http.MethodGet && r.URL.Path == "/latest/meta-data/iam/security-credentials/":
			if r.Header.Get("X-aws-ec2-metadata-token") != "fake-imdsv2-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte("PortainerBackupRole"))
		case r.Method == http.MethodGet && strings.TrimPrefix(r.URL.Path, "/latest/meta-data/iam/security-credentials/") == "PortainerBackupRole":
			if r.Header.Get("X-aws-ec2-metadata-token") != "fake-imdsv2-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"Code":"Success","Type":"AWS-HMAC","AccessKeyId":"` + credentials.AccessKeyID + `","SecretAccessKey":"` + credentials.SecretAccessKey + `","Token":"` + credentials.SessionToken + `","Expiration":"` + credentials.Expires.Format(time.RFC3339) + `"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(endpoint.Close)
	t.Setenv("AWS_EC2_METADATA_DISABLED", "false")
	t.Setenv("AWS_EC2_METADATA_SERVICE_ENDPOINT", endpoint.URL)

	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion("us-east-1"))
	require.NoError(t, err)
	require.NoError(t, checkRoleSources(cfg.Credentials))

	resolved, err := cfg.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("unable to get fake EC2 role credentials after %d requests %v: %v", requestCount, requestPaths, err)
	}
	resolved, err = (roleCredentials{provider: cfg.Credentials}).Retrieve(context.Background())
	require.NoError(t, err)
	require.Equal(t, credentials.AccessKeyID, resolved.AccessKeyID)
	require.Equal(t, credentials.SessionToken, resolved.SessionToken)
	require.GreaterOrEqual(t, requestCount, 3)
}

func TestCheckRoleSourcesAcceptsRolesAnywhereCredentialProcess(t *testing.T) {
	credentialsPath := resetAWSConfigEnvironment(t)
	processPath := filepath.Join(t.TempDir(), "role-credentials.sh")
	credentials := newRoleCredentials()
	processOutput := `{"Version":1,"AccessKeyId":"` + credentials.AccessKeyID + `","SecretAccessKey":"` + credentials.SecretAccessKey + `","SessionToken":"` + credentials.SessionToken + `","Expiration":"` + credentials.Expires.Format(time.RFC3339) + `"}`
	processScript := "#!/bin/sh\nprintf '%s\\n' '" + processOutput + "'\n"
	require.NoError(t, os.WriteFile(processPath, []byte(processScript), 0700))
	require.NoError(t, os.WriteFile(credentialsPath, nil, 0600))
	configPath := os.Getenv("AWS_CONFIG_FILE")
	require.NoError(t, os.WriteFile(configPath, []byte("[default]\ncredential_process = "+processPath+"\n"), 0600))
	t.Setenv("AWS_PROFILE", "default")

	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion("us-east-1"))
	require.NoError(t, err)
	require.NoError(t, checkRoleSources(cfg.Credentials))

	resolved, err := (roleCredentials{provider: cfg.Credentials}).Retrieve(context.Background())
	require.NoError(t, err)
	require.Equal(t, credentials.AccessKeyID, resolved.AccessKeyID)
	require.Equal(t, credentials.SessionToken, resolved.SessionToken)
}

type routeS3RequestsToTestServer struct {
	endpoint *url.URL
}

func (transport routeS3RequestsToTestServer) RoundTrip(request *http.Request) (*http.Response, error) {
	if !strings.Contains(request.URL.Host, ".amazonaws.com") {
		return http.DefaultTransport.RoundTrip(request)
	}
	copyOfRequest := request.Clone(request.Context())
	requestURL := *request.URL
	requestURL.Scheme = transport.endpoint.Scheme
	requestURL.Host = transport.endpoint.Host
	copyOfRequest.URL = &requestURL
	copyOfRequest.Host = request.URL.Host
	return http.DefaultTransport.RoundTrip(copyOfRequest)
}

func newLocalS3UploadClient(t *testing.T, s3Handler http.Handler) (*http.Client, aws.Credentials) {
	t.Helper()
	resetAWSConfigEnvironment(t)
	credentials := newRoleCredentials()
	credentialEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"AccessKeyId":"` + credentials.AccessKeyID + `","SecretAccessKey":"` + credentials.SecretAccessKey + `","Token":"` + credentials.SessionToken + `","Expiration":"` + credentials.Expires.Format(time.RFC3339) + `"}`))
	}))
	t.Cleanup(credentialEndpoint.Close)
	t.Setenv("AWS_CONTAINER_CREDENTIALS_FULL_URI", credentialEndpoint.URL)

	s3Endpoint := httptest.NewServer(s3Handler)
	t.Cleanup(s3Endpoint.Close)
	s3EndpointURL, err := url.Parse(s3Endpoint.URL)
	require.NoError(t, err)
	client := &http.Client{Transport: routeS3RequestsToTestServer{endpoint: s3EndpointURL}}
	return client, credentials
}

func TestUploadArchiveUsesTemporaryRoleAndUploadsObject(t *testing.T) {
	var putBody []byte
	var putHost, putPath, authorization, sessionToken, contentType string
	putCount := 0
	client, credentials := newLocalS3UploadClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		putCount++
		putHost = r.Host
		putPath = r.URL.Path
		authorization = r.Header.Get("Authorization")
		sessionToken = r.Header.Get("X-Amz-Security-Token")
		contentType = r.Header.Get("Content-Type")
		putBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	archivePath := filepath.Join(t.TempDir(), "portainer-backup.tar.gz")
	require.NoError(t, os.WriteFile(archivePath, []byte("backup archive bytes"), 0600))
	key, err := uploadArchiveWithHTTPClient(context.Background(), Configuration{
		BucketName: "portainer-backups",
		Region:     "ap-southeast-1",
		Prefix:     "daily",
	}, archivePath, client)
	if err != nil {
		t.Fatalf("upload failed: %v; observed %d S3 requests to %q", err, putCount, putPath)
	}
	require.NotEmpty(t, key)
	require.Contains(t, key, "daily/portainer-backup_")
	require.Equal(t, "portainer-backups.s3.ap-southeast-1.amazonaws.com", putHost)
	require.Equal(t, "/"+key, putPath)
	require.Contains(t, string(putBody), "backup archive bytes")
	require.Contains(t, authorization, "AWS4-HMAC-SHA256")
	require.Contains(t, authorization, credentials.AccessKeyID)
	require.Equal(t, credentials.SessionToken, sessionToken)
	require.True(t, strings.HasPrefix(contentType, "application/gzip"))
}

func TestUploadArchiveReportsS3ErrorCodeWithoutCredentialDetails(t *testing.T) {
	requests := 0
	client, credentials := newLocalS3UploadClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>"))
	}))
	archivePath := filepath.Join(t.TempDir(), "portainer-backup.tar.gz")
	require.NoError(t, os.WriteFile(archivePath, []byte("archive"), 0600))

	key, err := uploadArchiveWithHTTPClient(context.Background(), Configuration{
		BucketName: "portainer-backups",
		Region:     "ap-southeast-1",
	}, archivePath, client)
	require.Empty(t, key)
	require.ErrorContains(t, err, "AccessDenied")
	require.NotContains(t, err.Error(), credentials.SecretAccessKey)
	require.Equal(t, 1, requests)
}
