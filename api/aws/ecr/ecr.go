package ecr

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
)

// Registry represents an ECR registry endpoint information.
// This struct is used to parse and validate ECR endpoint URLs.
type Registry struct {
	ID     string // AWS account ID (empty for accountless endpoints like "ecr-fips.us-west-1.amazonaws.com")
	FIPS   bool   // Whether this is a FIPS endpoint (contains "-fips" in the URL)
	Region string // AWS region (e.g., "us-east-1", "us-gov-west-1")
	Public bool   // Whether this is ecr-public.aws.com
}

type Service struct {
	client *ecr.Client
}

func NewService(ctx context.Context, accessKey, secretKey, region string, useIAMRole bool) (*Service, error) {
	if !useIAMRole {
		return &Service{client: ecr.New(ecr.Options{
			Region:      region,
			Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
		})}, nil
	}

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, err
	}
	// The server's default chain may also contain long-lived keys. Only accept
	// temporary session credentials when the registry explicitly uses a role.
	provider := cfg.Credentials
	cfg.Credentials = aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
		creds, err := provider.Retrieve(ctx)
		if err == nil && creds.SessionToken == "" {
			return aws.Credentials{}, errors.New("ECR IAM role authentication requires temporary AWS credentials on the Portainer Server")
		}
		return creds, err
	})
	return &Service{client: ecr.NewFromConfig(cfg)}, nil
}
