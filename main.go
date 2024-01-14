package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/stssk/gh-reject/models"

	"github.com/AlecAivazis/survey/v2"
	"github.com/cli/go-gh"
	"github.com/cli/go-gh/pkg/api"
	"github.com/cli/go-gh/pkg/repository"
)

func main() {
	client, err := gh.RESTClient(nil)
	if err != nil {
		fmt.Println(err)
		return
	}

	currentRepo, err := gh.CurrentRepository()
	if err != nil {
		fmt.Println(err)
		return
	}

	pendingRuns, answer, shouldReturn := selectPendingRuns(client, currentRepo)
	if shouldReturn {
		return
	}

	pendingDeployments, rejectedEnvironments, shouldReturn := selectPendingDeployments(client, currentRepo, pendingRuns, answer)
	if shouldReturn {
		return
	}

	rejectDeployments(rejectedEnvironments, pendingDeployments, client, currentRepo, pendingRuns, answer)
}

// Fetch pending runs and prompt the user to select one of them
func selectPendingRuns(client api.RESTClient, currentRepo repository.Repository) ([]models.WorkflowRun, int, bool) {
	runs := models.Runs{}
	err := client.Get(models.RunsUrl(currentRepo.Owner(), currentRepo.Name())+"?status=waiting", &runs)
	if err != nil {
		fmt.Println(err)
		return nil, 0, true
	}
	if runs.TotalCount == 0 {
		fmt.Println("No runs detected")
		return nil, 0, true
	}
	pendingRuns := make([]models.WorkflowRun, 0)
	pendingRunsTexts := make([]string, 0)
	now := time.Now()
	for _, run := range runs.WorkflowRuns {
		pendingRuns = append(pendingRuns, run)
		pendingRunsTexts = append(pendingRunsTexts, fmt.Sprintf("%s, %s (%s) %s ago", run.DisplayTitle, run.Name, run.HeadBranch, now.Sub(run.RunStartedAt).Round(time.Second)))
	}
	promptRuns := &survey.Select{
		Message: "Select a workflow run",
		Options: pendingRunsTexts,
	}

	answer := -1
	survey.AskOne(promptRuns, &answer, survey.WithValidator(survey.Required))
	if answer < 0 {
		return nil, 0, true
	}
	return pendingRuns, answer, false
}

// Fetch pending deployments for a single run and prompt the user to select 0, 1 or more of them
func selectPendingDeployments(client api.RESTClient, currentRepo repository.Repository, pendingRuns []models.WorkflowRun, answer int) (models.PendingDeployments, []int, bool) {
	pendingDeployments := models.PendingDeployments{}
	err := client.Get(models.PendingDeploymentsUrl(currentRepo.Owner(), currentRepo.Name(), pendingRuns[answer].ID), &pendingDeployments)
	if err != nil {
		fmt.Println(err)
		return nil, nil, true
	}

	pendingDeploymentTexts := make([]string, len(pendingDeployments))
	for i, d := range pendingDeployments {
		pendingDeploymentTexts[i] = d.Environment.Name
	}

	rejectedEnvironments := []int{}
	promptDeployments := &survey.MultiSelect{
		Message: "Select environments to reject",
		Options: pendingDeploymentTexts,
	}
	survey.AskOne(promptDeployments, &rejectedEnvironments)

	if len(rejectedEnvironments) == 0 {
		return nil, nil, true
	}
	return pendingDeployments, rejectedEnvironments, false
}

// Send the approval request to GitHub actions
func rejectDeployments(rejectedEnvironments []int, pendingDeployments models.PendingDeployments, client api.RESTClient, currentRepo repository.Repository, pendingRuns []models.WorkflowRun, answer int) {
	rejectedIds := make([]int, len(rejectedEnvironments))
	for i, e := range rejectedEnvironments {
		rejectedIds[i] = pendingDeployments[e].Environment.ID
	}

	rejectRequest := models.RequestRejection{
		EnvironmentIds: rejectedIds,
		State:          models.Rejected,
	}
	req, err := json.Marshal(rejectRequest)
	if err != nil {
		fmt.Println(err)
		return
	}
	rejectedDeployments := models.DeploymentResponse{}
	err = client.Post(models.PendingDeploymentsUrl(currentRepo.Owner(), currentRepo.Name(), pendingRuns[answer].ID), bytes.NewReader(req), &rejectedDeployments)
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, e := range rejectedDeployments {
		fmt.Printf(" • %s rejected @%s\n", e.Environment, e.CreatedAt)
	}
}
