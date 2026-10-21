package task

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/project"
)

func TestDeliveryValidation(t *testing.T) {
	input := SubmitDeliveryInput{DeliveryInput: DeliveryInput{Identity: auth.Identity{UserID: "alice"}}, ReviewerUserID: "bob", Body: "  خروجی آماده است 👋  "}
	normalized, err := normalizeDelivery(input)
	if err != nil || normalized.Body != "خروجی آماده است 👋" {
		t.Fatalf("normalize: %+v %v", normalized, err)
	}
	for _, body := range []string{"", " \n\t ", "invalid\x00body", strings.Repeat("س", 10001)} {
		invalid := input
		invalid.Body = body
		if _, err := normalizeDelivery(invalid); !errors.Is(err, ErrInvalidDelivery) {
			t.Fatalf("body accepted: %v", err)
		}
	}
	input.Body = strings.Repeat("👋", 10000)
	if _, err := normalizeDelivery(input); err != nil {
		t.Fatalf("Unicode boundary: %v", err)
	}
	for _, link := range []string{"javascript:alert(1)", "/relative", "https://", "https://user:secret@example.com", "https://example.com/" + strings.Repeat("x", 2048)} {
		input.OutputURL = &link
		if _, err := normalizeDelivery(input); !errors.Is(err, ErrInvalidDelivery) {
			t.Fatalf("URL %q accepted: %v", link, err)
		}
	}
	link := " https://example.com/output?version=2 "
	input.OutputURL = &link
	got, err := normalizeDelivery(input)
	if err != nil || got.OutputURL == nil || *got.OutputURL != strings.TrimSpace(link) {
		t.Fatal("valid output URL rejected")
	}
	input.OutputURL = nil
	for _, reviewer := range []string{"", "  ", " alice "} {
		input.ReviewerUserID = reviewer
		if _, err := normalizeDelivery(input); !errors.Is(err, ErrInvalidDeliveryReviewer) {
			t.Fatal("empty/self reviewer accepted")
		}
	}
}

func TestDeliveryReviewValidation(t *testing.T) {
	input := ReviewDeliveryInput{DeliveryID: uuid.NewString(), Decision: DeliveryChangesRequested}
	if _, err := normalizeDeliveryReview(input, false); !errors.Is(err, ErrInvalidDeliveryReview) {
		t.Fatal("return without reason accepted")
	}
	input.Note = "  معیارهای پذیرش کامل نیست  "
	got, err := normalizeDeliveryReview(input, false)
	if err != nil || got.Note != strings.TrimSpace(input.Note) {
		t.Fatal("valid return rejected")
	}
	input.Decision, input.Note = DeliveryApproved, ""
	if _, err := normalizeDeliveryReview(input, false); err != nil {
		t.Fatal(err)
	}
	input.Decision = DeliveryWithdrawn
	if _, err := normalizeDeliveryReview(input, false); !errors.Is(err, ErrInvalidDeliveryReview) {
		t.Fatal("review endpoint can withdraw")
	}
	if got, err := normalizeDeliveryReview(input, true); err != nil || got.Decision != DeliveryWithdrawn {
		t.Fatal("withdraw rejected")
	}
	input.Note = strings.Repeat("س", 2001)
	if _, err := normalizeDeliveryReview(input, true); !errors.Is(err, ErrInvalidDeliveryReview) {
		t.Fatal("oversized note accepted")
	}
}

func TestDeliveryOwnership(t *testing.T) {
	alice := auth.Identity{UserID: "alice"}
	assignee := "alice"
	item := Task{AssigneeUserID: &assignee, CreatedByUserID: "creator"}
	if !canSubmitDelivery(alice, project.MemberRoleMember, item) {
		t.Fatal("assignee cannot submit")
	}
	if canSubmitDelivery(auth.Identity{UserID: "other"}, project.MemberRoleMember, item) {
		t.Fatal("unrelated member can submit")
	}
	if canSubmitDelivery(alice, project.MemberRoleViewer, item) {
		t.Fatal("viewer can submit")
	}
	if !canSubmitDelivery(auth.Identity{UserID: "manager"}, project.MemberRoleManager, item) {
		t.Fatal("manager cannot submit")
	}
	if !canSubmitDelivery(auth.Identity{Role: auth.RoleAdmin}, "", item) {
		t.Fatal("admin cannot submit")
	}
	item.AssigneeUserID = nil
	if canSubmitDelivery(alice, project.MemberRoleMember, item) || !canSubmitDelivery(auth.Identity{UserID: "creator"}, project.MemberRoleMember, item) {
		t.Fatal("unassigned ownership incorrect")
	}
	delivery := Delivery{SubmittedByUserID: "alice", ReviewerUserID: "bob"}
	if canResolveDelivery(alice, project.MemberRoleMember, delivery, false) {
		t.Fatal("self review allowed")
	}
	if canResolveDelivery(auth.Identity{UserID: "manager"}, project.MemberRoleManager, delivery, false) {
		t.Fatal("non-designated manager can review")
	}
	if !canResolveDelivery(auth.Identity{UserID: "bob"}, project.MemberRoleMember, delivery, false) {
		t.Fatal("reviewer denied")
	}
	if canResolveDelivery(auth.Identity{UserID: "bob"}, project.MemberRoleViewer, delivery, false) {
		t.Fatal("demoted reviewer can review")
	}
	if !canResolveDelivery(alice, project.MemberRoleMember, delivery, true) || !canResolveDelivery(auth.Identity{UserID: "manager"}, project.MemberRoleManager, delivery, true) {
		t.Fatal("submitter/manager cannot withdraw")
	}
	if canResolveDelivery(auth.Identity{UserID: "bob"}, project.MemberRoleMember, delivery, true) {
		t.Fatal("unrelated reviewer can withdraw")
	}
}
