package comment

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"

	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/notification"
	"github.com/mhdph/taskmanager/internal/project"
	"github.com/mhdph/taskmanager/internal/task"
)

type Service struct {
	repository *Repository

	taskRepository *task.Repository

	projectRepository *project.Repository

	projectAuthorizer *project.Authorizer

	notificationSender NotificationSender
}

func NewService(
	repository *Repository,
	taskRepository *task.Repository,
	projectRepository *project.Repository,
	projectAuthorizer *project.Authorizer,
	notificationSender NotificationSender,
) *Service {
	return &Service{
		repository: repository,

		taskRepository: taskRepository,

		projectRepository: projectRepository,

		projectAuthorizer: projectAuthorizer,

		notificationSender: notificationSender,
	}
}

type NotificationSender interface {
	Send(
		ctx context.Context,
		input notification.SendInput,
	) error
}

type CreateInput struct {
	Identity auth.Identity

	ProjectID string
	TaskID    string

	Body string

	MentionedUserIDs []string
}

func (s *Service) Create(
	ctx context.Context,
	input CreateInput,
) (Comment, error) {
	err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionCreateTaskComment,
	)
	if err != nil {
		return Comment{}, fmt.Errorf(
			"authorize comment creation: %w",
			err,
		)
	}

	_, err = s.taskRepository.GetForProject(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.TaskID,
	)
	if err != nil {
		return Comment{}, fmt.Errorf(
			"get task: %w",
			err,
		)
	}

	body := strings.TrimSpace(
		input.Body,
	)

	if body == "" ||
		len(body) > 10000 {

		return Comment{},
			ErrInvalidBody
	}

	mentions := make(
		[]string,
		0,
		len(input.MentionedUserIDs),
	)

	seen := make(
		map[string]struct{},
	)

	for _, userID := range input.MentionedUserIDs {

		userID = strings.TrimSpace(
			userID,
		)

		if userID == "" {
			continue
		}

		if _, exists :=
			seen[userID]; exists {

			continue
		}

		_, err :=
			s.projectRepository.
				GetMemberRole(
					ctx,
					input.Identity.TenantID,
					input.ProjectID,
					userID,
				)

		if err != nil {
			return Comment{},
				ErrMentionedUserNotMember
		}

		seen[userID] = struct{}{}

		mentions = append(
			mentions,
			userID,
		)
	}

	result, err :=
		s.repository.Create(
			ctx,
			CreateParams{
				ID: uuid.NewString(),

				TenantID: input.Identity.TenantID,

				ProjectID: input.ProjectID,

				TaskID: input.TaskID,

				AuthorUserID: input.Identity.UserID,

				Body: body,

				MentionedUserIDs: mentions,
			},
		)

	if err != nil {
		return Comment{}, fmt.Errorf(
			"create comment: %w",
			err,
		)
	}

	for _, mentionedUserID := range result.MentionedUserIDs {

		if mentionedUserID ==
			input.Identity.UserID {

			continue
		}

		actorUserID :=
			input.Identity.UserID

		projectID :=
			input.ProjectID

		taskID :=
			input.TaskID

		commentID :=
			result.ID

		notificationBody :=
			result.Body

		err := s.notificationSender.Send(
			ctx,
			notification.SendInput{
				TenantID: input.Identity.TenantID,

				RecipientUserID: mentionedUserID,

				Type: notification.EventCommentMentioned,

				ActorUserID: &actorUserID,

				ProjectID: &projectID,

				TaskID: &taskID,

				CommentID: &commentID,

				Title: "You were mentioned in a comment",

				Body: &notificationBody,

				Data: map[string]any{
					"commentId": result.ID,

					"taskId": input.TaskID,

					"projectId": input.ProjectID,
				},
			},
		)

		if err != nil {
			// the failed notification not caused the comment failed
			log.Printf(
				"send mention notification: %v",
				err,
			)
		}
	}

	return result, nil
}

type ListInput struct {
	Identity auth.Identity

	ProjectID string
	TaskID    string

	Limit  int
	Cursor string
}

func (s *Service) List(
	ctx context.Context,
	input ListInput,
) (CommentPage, error) {
	err := s.projectAuthorizer.Require(
		ctx,
		input.Identity,
		input.ProjectID,
		project.PermissionViewTaskComments,
	)
	if err != nil {
		return CommentPage{}, fmt.Errorf("authorize comment list: %w", err)
	}

	if _, err := s.taskRepository.GetForProject(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.TaskID,
	); err != nil {
		return CommentPage{}, fmt.Errorf("get task: %w", err)
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var cursor *Cursor
	if input.Cursor != "" {
		decoded, err := DecodeCursor(input.Cursor)
		if err != nil {
			return CommentPage{}, fmt.Errorf("invalid cursor: %w", err)
		}
		cursor = &decoded
	}

	items, err := s.repository.ListForTask(
		ctx,
		input.Identity.TenantID,
		input.ProjectID,
		input.TaskID,
		ListOptions{Limit: int32(limit + 1), Cursor: cursor},
	)
	if err != nil {
		return CommentPage{}, fmt.Errorf("list comments: %w", err)
	}

	hasNext := len(items) > limit
	if hasNext {
		items = items[:limit]
	}

	var nextCursor *string
	if hasNext && len(items) > 0 {
		last := items[len(items)-1]
		value := EncodeCursor(Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
		nextCursor = &value
	}

	return CommentPage{Items: items, NextCursor: nextCursor}, nil
}

func (s *Service) Delete(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	taskID string,
	commentID string,
) error {
	err := s.projectAuthorizer.Require(
		ctx,
		identity,
		projectID,
		project.PermissionDeleteTaskComment,
	)
	if err != nil {
		return fmt.Errorf("authorize comment deletion: %w", err)
	}

	if _, err := s.taskRepository.GetForProject(
		ctx,
		identity.TenantID,
		projectID,
		taskID,
	); err != nil {
		return fmt.Errorf("get task: %w", err)
	}

	if err := s.repository.Delete(
		ctx,
		identity.TenantID,
		projectID,
		taskID,
		commentID,
	); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	return nil
}
