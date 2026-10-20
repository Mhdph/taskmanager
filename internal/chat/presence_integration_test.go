package chat

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mhdph/taskmanager/internal/auth"
)

func TestSharedPresenceLeasesAndTransitions(t *testing.T) {
	repository, pool, ctx := integrationChatRepository(t)

	const tenant = "tenant-a"

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob")

	detail, err := repository.CreateGroupConversation(
		ctx,
		CreateGroupConversationParams{
			ID:              uuid.NewString(),
			TenantID:        tenant,
			Title:           "shared presence",
			CreatedByUserID: "alice",
			MemberIDs:       []string{"bob"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	service := NewService(
		repository,
		nil,
		nil,
		nil,
		nil,
		0,
		repository,
		nil,
	)

	alice := auth.Identity{TenantID: tenant, UserID: "alice"}

	first := PresenceLease{
		ConnectionID: uuid.NewString(),
		TenantID:     tenant,
		UserID:       "bob",
		InstanceID:   "instance-a",
		TTL:          time.Minute,
	}
	second := PresenceLease{
		ConnectionID: uuid.NewString(),
		TenantID:     tenant,
		UserID:       "bob",
		InstanceID:   "instance-b",
		TTL:          time.Minute,
	}

	changed, err := repository.AcquirePresence(ctx, first)
	if err != nil || !changed {
		t.Fatalf("first acquire: changed=%v err=%v", changed, err)
	}

	// A second connection on another instance does not republish online.
	changed, err = repository.AcquirePresence(ctx, second)
	if err != nil || changed {
		t.Fatalf("second acquire: changed=%v err=%v", changed, err)
	}

	online, err := repository.IsUserOnline(ctx, tenant, "bob")
	if err != nil || !online {
		t.Fatalf("bob online: online=%v err=%v", online, err)
	}

	userIDs, err := repository.OnlineUserIDs(
		ctx,
		tenant,
		[]string{"alice", "bob"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(userIDs) != 1 || userIDs[0] != "bob" {
		t.Fatalf("online users = %v, want [bob]", userIDs)
	}

	members, err := service.OnlineMemberIDs(ctx, alice, detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0] != "bob" {
		t.Fatalf("conversation presence = %v, want [bob]", members)
	}

	// One connection closing leaves the user online on the other instance.
	changed, err = repository.ReleasePresence(
		ctx,
		first.ConnectionID,
		tenant,
		"bob",
	)
	if err != nil || changed {
		t.Fatalf("first release: changed=%v err=%v", changed, err)
	}

	online, err = repository.IsUserOnline(ctx, tenant, "bob")
	if err != nil || !online {
		t.Fatalf("bob after first release: online=%v err=%v", online, err)
	}

	// The last connection closes: exactly one offline transition, even if
	// release is retried.
	changed, err = repository.ReleasePresence(
		ctx,
		second.ConnectionID,
		tenant,
		"bob",
	)
	if err != nil || !changed {
		t.Fatalf("last release: changed=%v err=%v", changed, err)
	}

	changed, err = repository.ReleasePresence(
		ctx,
		second.ConnectionID,
		tenant,
		"bob",
	)
	if err != nil || changed {
		t.Fatalf("repeated release: changed=%v err=%v", changed, err)
	}

	online, err = repository.IsUserOnline(ctx, tenant, "bob")
	if err != nil || online {
		t.Fatalf("bob after last release: online=%v err=%v", online, err)
	}
}

func TestPresenceHeartbeatAndSweep(t *testing.T) {
	repository, pool, ctx := integrationChatRepository(t)

	const tenant = "tenant-a"

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob")

	lease := PresenceLease{
		ConnectionID: uuid.NewString(),
		TenantID:     tenant,
		UserID:       "bob",
		InstanceID:   "instance-a",
		TTL:          500 * time.Millisecond,
	}

	if changed, err := repository.AcquirePresence(
		ctx,
		lease,
	); err != nil || !changed {
		t.Fatalf("acquire: changed=%v err=%v", changed, err)
	}

	expiresAt := func() time.Time {
		t.Helper()

		var value time.Time

		if err := pool.QueryRow(
			ctx,
			`SELECT expires_at
			 FROM chat_presence_leases
			 WHERE connection_id = $1::uuid`,
			lease.ConnectionID,
		).Scan(&value); err != nil {
			t.Fatal(err)
		}

		return value
	}

	before := expiresAt()

	time.Sleep(50 * time.Millisecond)

	found, err := repository.RefreshPresence(
		ctx,
		lease.ConnectionID,
		lease.InstanceID,
		lease.TTL,
	)
	if err != nil || !found {
		t.Fatalf("heartbeat: found=%v err=%v", found, err)
	}

	if after := expiresAt(); !after.After(before) {
		t.Fatalf("heartbeat did not extend lease: before=%v after=%v", before, after)
	}

	online, err := repository.IsUserOnline(ctx, tenant, "bob")
	if err != nil || !online {
		t.Fatalf("bob after heartbeat: online=%v err=%v", online, err)
	}

	// Expire the lease deterministically instead of sleeping for the TTL.
	if _, err := pool.Exec(
		ctx,
		`UPDATE chat_presence_leases
		 SET expires_at = NOW() - INTERVAL '1 second'
		 WHERE connection_id = $1::uuid`,
		lease.ConnectionID,
	); err != nil {
		t.Fatal(err)
	}

	updates, err := repository.SweepExpiredPresence(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 ||
		updates[0].TenantID != tenant ||
		updates[0].UserID != "bob" ||
		updates[0].Online {
		t.Fatalf("sweep updates = %+v, want one offline transition", updates)
	}

	// A concurrent sweeper that runs after the transition is a no-op.
	updates, err = repository.SweepExpiredPresence(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 0 {
		t.Fatalf("second sweep published again: %+v", updates)
	}

	found, err = repository.RefreshPresence(
		ctx,
		lease.ConnectionID,
		lease.InstanceID,
		lease.TTL,
	)
	if err != nil || found {
		t.Fatalf("refresh after expiry: found=%v err=%v", found, err)
	}
}

func TestHubPresenceIsSharedAcrossInstances(t *testing.T) {
	repository, pool, ctx := integrationChatRepository(t)

	const tenant = "tenant-a"

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob")

	if _, err := repository.CreateGroupConversation(
		ctx,
		CreateGroupConversationParams{
			ID:              uuid.NewString(),
			TenantID:        tenant,
			Title:           "two instances",
			CreatedByUserID: "alice",
			MemberIDs:       []string{"bob"},
		},
	); err != nil {
		t.Fatal(err)
	}

	firstPublisher := &recordingPublisher{}
	secondPublisher := &recordingPublisher{}

	firstInstance := NewHub(repository, firstPublisher)
	secondInstance := NewHub(repository, secondPublisher)

	firstClient := &wsClient{
		tenantID: tenant,
		userID:   "bob",
		send:     make(chan []byte, 4),
	}
	secondClient := &wsClient{
		tenantID: tenant,
		userID:   "bob",
		send:     make(chan []byte, 4),
	}

	firstInstance.Register(ctx, firstClient)
	secondInstance.Register(ctx, secondClient)

	if len(firstPublisher.events) != 1 ||
		firstPublisher.events[0].Kind != EventPresenceOnline {
		t.Fatalf("first instance events = %+v", firstPublisher.events)
	}
	if len(secondPublisher.events) != 0 {
		t.Fatalf("second connection republished online: %+v", secondPublisher.events)
	}

	// Closing one instance leaves the user online through the other.
	firstInstance.Close(ctx)

	online, err := repository.IsUserOnline(ctx, tenant, "bob")
	if err != nil || !online {
		t.Fatalf("bob after first instance close: online=%v err=%v", online, err)
	}
	if len(firstPublisher.events) != 1 {
		t.Fatalf("first close published offline early: %+v", firstPublisher.events)
	}

	secondInstance.Close(ctx)

	online, err = repository.IsUserOnline(ctx, tenant, "bob")
	if err != nil || online {
		t.Fatalf("bob after last instance close: online=%v err=%v", online, err)
	}
	if len(secondPublisher.events) != 1 ||
		secondPublisher.events[0].Kind != EventPresenceOffline {
		t.Fatalf("second instance events = %+v", secondPublisher.events)
	}
}

func TestChatNotificationsUseSharedPresence(t *testing.T) {
	repository, pool, ctx := integrationChatRepository(t)

	const tenant = "tenant-a"

	addTenantMembers(t, pool, ctx, tenant, "alice", "bob", "carol")

	sender := &recordingNotificationSender{}

	service := NewService(
		repository,
		nil,
		nil,
		nil,
		nil,
		0,
		repository,
		sender,
	)

	alice := auth.Identity{TenantID: tenant, UserID: "alice"}

	detail, err := service.CreateGroup(ctx, CreateGroupInput{
		Identity:  alice,
		Title:     "shared notifications",
		MemberIDs: []string{"bob", "carol"},
	})
	if err != nil {
		t.Fatal(err)
	}

	lease := PresenceLease{
		ConnectionID: uuid.NewString(),
		TenantID:     tenant,
		UserID:       "bob",
		InstanceID:   "another-instance",
		TTL:          time.Minute,
	}

	if changed, err := repository.AcquirePresence(
		ctx,
		lease,
	); err != nil || !changed {
		t.Fatalf("acquire bob presence: changed=%v err=%v", changed, err)
	}

	if _, _, err := service.SendMessage(ctx, SendMessageInput{
		Identity:       alice,
		ConversationID: detail.ID,
		Body:           "first",
	}); err != nil {
		t.Fatal(err)
	}

	notifications := sender.snapshot()
	if len(notifications) != 1 ||
		notifications[0].RecipientUserID != "carol" {
		t.Fatalf(
			"notifications with bob online elsewhere = %+v, want only carol",
			notifications,
		)
	}

	if changed, err := repository.ReleasePresence(
		ctx,
		lease.ConnectionID,
		tenant,
		"bob",
	); err != nil || !changed {
		t.Fatalf("release bob presence: changed=%v err=%v", changed, err)
	}

	if _, _, err := service.SendMessage(ctx, SendMessageInput{
		Identity:       alice,
		ConversationID: detail.ID,
		Body:           "second",
	}); err != nil {
		t.Fatal(err)
	}

	notifications = sender.snapshot()[1:]
	if len(notifications) != 2 {
		t.Fatalf("notifications after bob went offline = %+v", notifications)
	}

	recipients := map[string]bool{}
	for _, item := range notifications {
		recipients[item.RecipientUserID] = true
	}

	if !recipients["bob"] || !recipients["carol"] {
		t.Fatalf("recipients = %v, want bob and carol", recipients)
	}
}
