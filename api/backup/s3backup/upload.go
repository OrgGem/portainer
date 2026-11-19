package s3backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/google/uuid"
)

const maxPutObjectSize = int64(5 * 1024 * 1024 * 1024)

var errorCodePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// roleCredentials validates every refresh, not just the first credentials
// obtained when a client is created.
type roleCredentials struct {
	provider aws.CredentialsProvider
}

func (p roleCredentials) Retrieve(ctx context.Context) (aws.Credentials, error) {
	credentials, err := p.provider.Retrieve(ctx)
	if err != nil {
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return aws.Credentials{}, fmt.Errorf("AWS role credential request was cancelled or timed out: %w", ctx.Err())
		}
		return aws.Credentials{}, errors.New("unable to obtain AWS role credentials; check the server credential provider")
	}
	if !credentials.HasKeys() || !credentials.CanExpire || credentials.SessionToken == "" || credentials.Expired() {
		return aws.Credentials{}, errors.New("AWS role provider must return unexpired temporary session credentials")
	}
	return credentials, nil
}

// checkRoleSources prevents the SDK's fallback chain from silently selecting
// credentials stored in environment variables or a shared credentials file.
func checkRoleSources(provider aws.CredentialsProvider) error {
	sourced, ok := provider.(aws.CredentialProviderSource)
	if !ok {
		return errors.New("unable to determine AWS credential provider")
	}
	sources := sourced.ProviderSources()
	if len(sources) == 0 {
		return errors.New("unable to determine AWS credential provider")
	}
	for _, source := range sources {
		switch source {
		case aws.CredentialSourceEnvVarsSTSWebIDToken,
			aws.CredentialSourceSTSAssumeRole,
			aws.CredentialSourceSTSAssumeRoleWebID,
			aws.CredentialSourceProfileSourceProfile,
			aws.CredentialSourceProfileNamedProvider,
			aws.CredentialSourceProfileSTSWebIDToken,
			aws.CredentialSourceProfileProcess,
			aws.CredentialSourceProcess,
			aws.CredentialSourceHTTP,
			aws.CredentialSourceIMDS:
			// These sources obtain temporary credentials from a role provider.
		default:
			return errors.New("AWS static credentials are not supported; configure an IAM role provider")
		}
	}
	return nil
}

func uploadArchive(ctx context.Context, destination Configuration, archivePath string) (string, error) {
	return uploadArchiveWithHTTPClient(ctx, destination, archivePath, nil)
}

func uploadArchiveWithHTTPClient(ctx context.Context, destination Configuration, archivePath string, httpClient aws.HTTPClient) (string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", errors.New("unable to open the backup archive")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", errors.New("unable to read the backup archive")
	}
	if info.Size() > maxPutObjectSize {
		return "", errors.New("backup archive exceeds the 5 GiB S3 upload limit")
	}

	options := []func(*config.LoadOptions) error{config.WithRegion(destination.Region)}
	if httpClient != nil {
		options = append(options, config.WithHTTPClient(httpClient))
	}
	cfg, err := config.LoadDefaultConfig(ctx, options...)
	if err != nil {
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return "", fmt.Errorf("S3 backup was cancelled or timed out: %w", ctx.Err())
		}
		return "", errors.New("unable to load AWS role configuration; check the server credential provider")
	}
	if err := checkRoleSources(cfg.Credentials); err != nil {
		return "", err
	}
	provider := roleCredentials{provider: cfg.Credentials}
	if _, err := provider.Retrieve(ctx); err != nil {
		return "", err
	}
	cfg.Credentials = aws.NewCredentialsCache(provider)

	identifier, err := uuid.NewRandom()
	if err != nil {
		return "", errors.New("unable to generate a backup object name")
	}
	name := fmt.Sprintf("portainer-backup_%s_%s.tar.gz", time.Now().UTC().Format("20060102T150405Z"), identifier)
	key := name
	if destination.Prefix != "" {
		key = strings.TrimSuffix(destination.Prefix, "/") + "/" + name
	}

	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		// The destination is AWS S3. Ignore endpoint overrides from the runtime.
		options.BaseEndpoint = nil
	})
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(destination.BucketName),
		Key:           aws.String(key),
		Body:          file,
		ContentLength: aws.Int64(info.Size()),
		ContentType:   aws.String("application/gzip"),
		IfNoneMatch:   aws.String("*"),
	})
	if err != nil {
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return "", fmt.Errorf("S3 backup was cancelled or timed out: %w", ctx.Err())
		}
		var apiError smithy.APIError
		if errors.As(err, &apiError) && errorCodePattern.MatchString(apiError.ErrorCode()) {
			return "", fmt.Errorf("S3 rejected the backup upload (%s)", apiError.ErrorCode())
		}
		return "", errors.New("unable to upload the backup to S3; check network access, region, and bucket permissions")
	}
	return key, nil
}
