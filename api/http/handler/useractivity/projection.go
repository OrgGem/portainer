package useractivity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/dataservices"
)

type Activity struct {
	ID            string         `json:"id"`
	Timestamp     int64          `json:"timestamp"`
	Username      string         `json:"username"`
	Context       string         `json:"context"`
	Action        string         `json:"action"`
	Status        string         `json:"status"`
	Source        string         `json:"source"`
	EnvironmentID int            `json:"environmentId"`
	StackID       int            `json:"stackId"`
	StackName     string         `json:"stackName"`
	Payload       map[string]any `json:"payload"`
}

func activitySnapshot(ctx context.Context, store dataservices.DataStore) ([]Activity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("activity datastore unavailable")
	}
	var stacks []portainer.Stack
	var endpoints []portainer.Endpoint
	var users []portainer.User
	err := store.ViewTx(func(tx dataservices.DataStoreTx) error {
		var err error
		if stacks, err = tx.Stack().ReadAll(); err != nil {
			return err
		}
		if endpoints, err = tx.Endpoint().ReadAll(); err != nil {
			return err
		}
		users, err = tx.User().ReadAll()
		return err
	})
	if err != nil {
		return nil, err
	}
	environments := make(map[portainer.EndpointID]string, len(endpoints))
	for _, endpoint := range endpoints {
		environments[endpoint.ID] = endpoint.Name
	}
	usernames := make(map[portainer.UserID]string, len(users))
	for _, user := range users {
		usernames[user.ID] = user.Username
	}
	rows := make([]Activity, 0)
	for _, stack := range stacks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		base := Activity{Context: environments[stack.EndpointID], EnvironmentID: int(stack.EndpointID), StackID: int(stack.ID), StackName: stack.Name}
		if stack.CreationDate > 0 {
			row := base
			row.Source = "metadata"
			row.Action = "create"
			row.Status = "unknown"
			row.Timestamp = stack.CreationDate
			row.Username = stack.CreatedBy
			row.Payload = summary("Stack creation metadata")
			row.ID = activityID("metadata", stack.ID, "create", stack.CreationDate)
			rows = append(rows, row)
		}
		if stack.UpdateDate > 0 {
			row := base
			row.Source = "metadata"
			row.Action = "update"
			row.Status = "unknown"
			row.Timestamp = stack.UpdateDate
			row.Username = stack.UpdatedBy
			row.Payload = summary("Stack update metadata")
			row.ID = activityID("metadata", stack.ID, "update", stack.UpdateDate)
			rows = append(rows, row)
		}
		if stack.Configuration != nil {
			duplicates := make(map[string]int)
			for _, event := range stack.Configuration.Events {
				if event.Time <= 0 {
					continue
				}
				action := event.Action
				if !oneOf(action, "upload", "apply", "rollback", "recover") {
					action = "unknown"
				}
				identity := fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%t", event.RevisionID, event.Action, event.UserID, event.Time, event.Success)
				ordinal := duplicates[identity]
				duplicates[identity]++
				row := base
				row.Source = "configuration"
				row.Action = action
				row.Timestamp = event.Time
				row.Username = usernames[event.UserID]
				row.Status = "failure"
				if event.Success {
					row.Status = "success"
				}
				row.Payload = summary("Configuration " + action)
				row.ID = activityID("configuration", stack.ID, identity, ordinal)
				rows = append(rows, row)
			}
		}
		for index, deployment := range stack.DeploymentStatus {
			if deployment.Time <= 0 {
				continue
			}
			row := base
			row.Source = "deployment"
			row.Action = "deployment"
			row.Timestamp = deployment.Time
			text := "Deployment status unavailable"
			switch deployment.Status {
			case portainer.StackStatusActive:
				row.Status = "success"
				text = "Stack active"
			case portainer.StackStatusInactive:
				row.Status = "inactive"
				text = "Stack inactive"
			case portainer.StackStatusDeploying:
				row.Status = "in_progress"
				text = "Deployment in progress"
			case portainer.StackStatusError:
				row.Status = "failure"
				text = "Deployment failed"
			default:
				row.Status = "unknown"
			}
			row.Payload = summary(text)
			row.ID = activityID("deployment", stack.ID, index, deployment.Time, deployment.Status)
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func summary(text string) map[string]any { return map[string]any{"summary": text} }

func activityID(parts ...any) string {
	encoded, _ := json.Marshal(parts)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}
