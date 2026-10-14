package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/mhdph/taskmanager/internal/auth"
)

type Service struct {
	repository *Repository
}

func NewService(
	repository *Repository,
) *Service {
	return &Service{
		repository: repository,
	}
}

type Input struct {
	Identity auth.Identity

	Query string

	Type string

	Limit int
}

func (s *Service) Search(
	ctx context.Context,
	input Input,
) (Response, error) {
	query := strings.TrimSpace(
		input.Query,
	)

	if len(query) < 2 {
		return Response{},
			ErrInvalidQuery
	}

	searchType :=
		strings.ToLower(
			strings.TrimSpace(
				input.Type,
			),
		)

	if searchType == "" {
		searchType = "all"
	}

	switch searchType {
	case "all",
		"projects",
		"tasks":

	default:
		return Response{},
			ErrInvalidType
	}

	limit := input.Limit

	if limit <= 0 {
		limit = 20
	}

	if limit > 50 {
		limit = 50
	}

	result := Response{
		Projects: make([]Result, 0),

		Tasks: make([]Result, 0),
	}

	isAdmin :=
		input.Identity.IsAdmin()
	if searchType == "all" ||
		searchType == "projects" {

		projects, err :=
			s.repository.Projects(
				ctx,
				input.Identity.TenantID,
				input.Identity.UserID,
				isAdmin,
				query,
				int32(limit),
			)

		if err != nil {
			return Response{}, fmt.Errorf(
				"search projects: %w",
				err,
			)
		}

		result.Projects =
			projects
	}

	if searchType == "all" ||
		searchType == "tasks" {

		tasks, err :=
			s.repository.Tasks(
				ctx,
				input.Identity.TenantID,
				input.Identity.UserID,
				isAdmin,
				query,
				int32(limit),
			)

		if err != nil {
			return Response{}, fmt.Errorf(
				"search tasks: %w",
				err,
			)
		}

		result.Tasks =
			tasks
	}

	return result, nil
}
