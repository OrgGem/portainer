package stacks

import (
	"cmp"
	"context"
	"net/http"
	"strings"

	"github.com/pkg/errors"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/dataservices/source"
	gittypes "github.com/portainer/portainer/api/git/types"
	"github.com/portainer/portainer/api/gitops/sources"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
)

type gitSourceAccessStore interface {
	Source() dataservices.SourceService
	IsErrObjectNotFound(err error) bool
}

type stackGitConfigUpdateInput struct {
	SourceID                      portainer.SourceID
	RepositoryURL                 string
	RepositoryReferenceName       string
	ConfigFilePath                string
	TLSSkipVerify                 *bool
	RepositoryAuthentication      bool
	ClearRepositoryAuthentication bool
	RepositoryUsername            string
	RepositoryPassword            string
}

func isNonEmptyGitCredential(val string) bool {
	return strings.TrimSpace(val) != "" && val != redactedEnvPlaceholder
}

func sameNormalizedGitURL(a, b string) bool {
	if a == b {
		return true
	}
	normA, errA := gittypes.NormalizeURL(a)
	normB, errB := gittypes.NormalizeURL(b)
	if errA != nil || errB != nil {
		return false
	}
	return normA == normB
}

func resolveAdditionalFiles(r *http.Request, existing, incoming []string, clearAdditionalFiles bool) []string {
	if clearAdditionalFiles {
		return []string{}
	}
	if incoming == nil {
		return existing
	}
	if len(incoming) == 0 && isTokenAuthenticatedRequest(r) {
		return existing
	}
	return incoming
}

func resolveAutoUpdate(r *http.Request, existing, incoming *portainer.AutoUpdateSettings, clearAutoUpdate bool) *portainer.AutoUpdateSettings {
	if clearAutoUpdate {
		return nil
	}
	if incoming == nil && isTokenAuthenticatedRequest(r) {
		return existing
	}
	return incoming
}

func resolveStackPrune(stack *portainer.Stack, incoming *bool) bool {
	prune := stack.Option != nil && stack.Option.Prune
	if incoming != nil {
		prune = *incoming
	}
	return prune
}

func applyStackPruneOption(stack *portainer.Stack, prune bool) {
	if stack.Option != nil {
		stack.Option.Prune = prune
		return
	}
	stack.Option = &portainer.StackOption{
		Prune: prune,
	}
}

// applyStackGitConfigUpdate updates gitConfig safely without ever overwriting stored
// credentials with empty/redacted PAT or Username values:
//   - If SourceID != 0, validates and repoints to that Source.
//   - If PAT or Username is empty (and ClearRepositoryAuthentication is false), defaults
//     to the existing stored Source credentials instead of clearing Authentication.
//   - If a non-empty PAT is provided, verifies repository connectivity via gitService
//     BEFORE allowing any database write.
func applyStackGitConfigUpdate(
	ctx context.Context,
	gitService portainer.GitService,
	store gitSourceAccessStore,
	userContext source.UserContext,
	existingSourceID portainer.SourceID,
	gitConfig *gittypes.RepoConfig,
	input stackGitConfigUpdateInput,
) (newSourceID portainer.SourceID, effectiveSourceID portainer.SourceID, httpErr *httperror.HandlerError) {
	gitConfig.ReferenceName = cmp.Or(input.RepositoryReferenceName, gitConfig.ReferenceName)
	if input.ConfigFilePath != "" {
		gitConfig.ConfigFilePath = input.ConfigFilePath
	}

	if input.SourceID != 0 {
		src, err := sources.ValidateGitSourceAccess(store, userContext, input.SourceID)
		if err != nil {
			return 0, 0, err
		}
		if src.Git == nil {
			return 0, 0, httperror.BadRequest("Source has no git configuration", errors.New("source has no git config"))
		}
		return input.SourceID, input.SourceID, nil
	}

	hasUser := isNonEmptyGitCredential(input.RepositoryUsername)
	hasPass := isNonEmptyGitCredential(input.RepositoryPassword)
	urlChanged := input.RepositoryURL != "" && !sameNormalizedGitURL(input.RepositoryURL, gitConfig.URL)
	tlsChanged := input.TLSSkipVerify != nil && *input.TLSSkipVerify != gitConfig.TLSSkipVerify

	if input.ClearRepositoryAuthentication {
		if input.RepositoryURL != "" {
			gitConfig.URL = input.RepositoryURL
		}
		if input.TLSSkipVerify != nil {
			gitConfig.TLSSkipVerify = *input.TLSSkipVerify
		}
		gitConfig.Authentication = nil
		if err := verifyGitRepoConnection(ctx, gitService, gitConfig); err != nil {
			return 0, 0, err
		}
		return 0, existingSourceID, nil
	}

	if hasPass {
		if input.RepositoryURL != "" {
			gitConfig.URL = input.RepositoryURL
		}
		if input.TLSSkipVerify != nil {
			gitConfig.TLSSkipVerify = *input.TLSSkipVerify
		}

		candidateAuth := &gittypes.GitAuthentication{}
		if gitConfig.Authentication != nil {
			copied := *gitConfig.Authentication
			candidateAuth = &copied
		}
		if hasUser {
			candidateAuth.Username = input.RepositoryUsername
		}
		candidateAuth.Password = input.RepositoryPassword
		gitConfig.Authentication = candidateAuth

		// Verify Git connectivity with the new PAT BEFORE persisting/overwriting in DB.
		if err := verifyGitRepoConnection(ctx, gitService, gitConfig); err != nil {
			return 0, 0, err
		}

		return 0, existingSourceID, nil
	}

	// PAT is empty/omitted/redacted: never overwrite with an empty PAT.
	// If URL and TLS are unchanged, default directly to the existing stored Source
	// so SaveWorkflowArtifact only updates stack artifact fields (Ref/Path/Hash)
	// and leaves the stored Source credentials completely untouched.
	if !urlChanged && !tlsChanged {
		return existingSourceID, existingSourceID, nil
	}

	if input.RepositoryURL != "" {
		gitConfig.URL = input.RepositoryURL
	}
	if input.TLSSkipVerify != nil {
		gitConfig.TLSSkipVerify = *input.TLSSkipVerify
	}

	if err := verifyGitRepoConnection(ctx, gitService, gitConfig); err != nil {
		return 0, 0, err
	}

	return 0, existingSourceID, nil
}

func verifyGitRepoConnection(ctx context.Context, gitService portainer.GitService, gitConfig *gittypes.RepoConfig) *httperror.HandlerError {
	if gitService == nil || gitConfig == nil {
		return nil
	}

	var username, password string
	if gitConfig.Authentication != nil {
		username = gitConfig.Authentication.Username
		password = gitConfig.Authentication.Password
	}

	if _, err := gitService.LatestCommitID(
		ctx,
		gitConfig.URL,
		gitConfig.ReferenceName,
		username,
		password,
		gitConfig.TLSSkipVerify,
	); err != nil {
		return httperror.BadRequest("Unable to connect to Git repository with provided configuration", err)
	}

	return nil
}
