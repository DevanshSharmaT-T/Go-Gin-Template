// File: test/integration/messages_test.go

//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	authservice "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/auth/service"
	messagedomain "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/messages/domain"
	messageservice "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/messages/service"
	apperrors "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/mail"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/test/harness"
)

type messagesFixture struct {
	notifications *messageservice.NotificationService
	notifier      messagedomain.Notifier
	mailer        mail.Mailer
	auth          *authservice.AuthService
	db            *gorm.DB
}

func newMessagesFixture(t *testing.T) *messagesFixture {
	t.Helper()

	f := &messagesFixture{}
	harness.App(t, []any{&f.notifications, &f.notifier, &f.mailer, &f.auth, &f.db})

	t.Cleanup(func() {
		for _, table := range []string{"notifications", "outbound_mail", "verification_tokens", "users"} {
			if err := f.db.Exec("DELETE FROM " + table).Error; err != nil {
				t.Errorf("clearing %s: %v", table, err)
			}
		}
	})

	return f
}

// **The test that proves the decoration is applied at the right scope.**
//
// fx scopes a decoration to the module that declares it. If RecordMail were
// inside messages.Module rather than at the root, the auth module — the only
// thing that sends mail — would still hold the undecorated mailer, nothing
// would be recorded, and there would be no error anywhere to say so. The
// symptom would be exactly this table staying empty.
func TestMessages_SendingMailIsRecorded(t *testing.T) {
	f := newMessagesFixture(t)
	ctx := context.Background()

	username := unique("record")
	if _, err := f.auth.Register(ctx, &authservice.RegisterRequestDTO{
		Username: username, Email: username + "@example.com",
		Password: "a sufficiently long password", FirstName: "Rec", LastName: "Ord",
	}); err != nil {
		t.Fatalf("registering: %v", err)
	}

	result, err := f.notifications.ListOutboundMail(ctx, messagedomain.Page{})
	if err != nil {
		t.Fatalf("listing outbound mail: %v", err)
	}

	if result.Total == 0 {
		t.Fatal("registering sent a verification email but nothing was recorded; " +
			"the recording mailer is not decorating the mailer the auth module holds")
	}

	found := false
	for _, record := range result.Mail {
		if record.Recipient == username+"@example.com" {
			found = true
			if record.Status != messagedomain.StatusSent.String() {
				t.Errorf("want the send recorded as sent, got %q (%s)", record.Status, record.Failure)
			}
			if record.Subject == "" {
				t.Error("the record has no subject")
			}
		}
	}
	if !found {
		t.Fatalf("no record for the address that was mailed; got %d records", result.Total)
	}
}

// The record must not carry the message body, because every message this
// application sends contains a single-use link.
func TestMessages_TheDeliveryRecordCarriesNoBody(t *testing.T) {
	f := newMessagesFixture(t)
	ctx := context.Background()

	username := unique("nobody")
	if _, err := f.auth.Register(ctx, &authservice.RegisterRequestDTO{
		Username: username, Email: username + "@example.com",
		Password: "a sufficiently long password", FirstName: "No", LastName: "Body",
	}); err != nil {
		t.Fatalf("registering: %v", err)
	}

	// Read every column, not just the ones the DTO exposes: the claim is about
	// what is stored, not about what is serialised.
	var rows []map[string]any
	if err := f.db.Table("outbound_mail").Find(&rows).Error; err != nil {
		t.Fatalf("reading outbound_mail: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("nothing was recorded")
	}

	for _, row := range rows {
		for column := range row {
			switch column {
			case "body", "html", "text", "content", "token":
				t.Errorf("outbound_mail has a %q column; a delivery log holding message "+
					"bodies is a log of working single-use links", column)
			}
		}
	}
}

// A failed send is recorded as failed, so "did the reset link go out?" has an
// answer either way.
func TestMessages_AFailedSendIsRecorded(t *testing.T) {
	f := newMessagesFixture(t)
	ctx := context.Background()

	// An unencodable message: the recipient carries an injected header, which
	// the encoder refuses before anything reaches a server.
	err := f.mailer.Send(ctx, mail.Message{
		From:    mail.Address{Email: "no-reply@example.com"},
		To:      "someone@example.com\r\nBcc: attacker@example.com",
		Subject: "Injected",
		Text:    "body",
	})
	if err == nil {
		t.Fatal("an injected message was sent")
	}

	result, listErr := f.notifications.ListOutboundMail(ctx, messagedomain.Page{})
	if listErr != nil {
		t.Fatalf("listing outbound mail: %v", listErr)
	}
	if result.Total != 1 {
		t.Fatalf("want one record for the failed attempt, got %d", result.Total)
	}
	if result.Mail[0].Status != messagedomain.StatusFailed.String() {
		t.Fatalf("want the attempt recorded as failed, got %q", result.Mail[0].Status)
	}
	if result.Mail[0].Failure == "" {
		t.Fatal("a failed record has no reason")
	}
}

// Notifications are self-scoped. The user id is in the WHERE clause, so
// reaching somebody else's is not a check that can be forgotten.
func TestMessages_NotificationsAreScopedToTheirOwner(t *testing.T) {
	f := newMessagesFixture(t)
	ctx := context.Background()

	owner := uuid.New()
	other := uuid.New()

	if err := f.notifier.Notify(ctx, owner, messagedomain.KindInfo, "For the owner", "body"); err != nil {
		t.Fatalf("notifying: %v", err)
	}

	ownerList, err := f.notifications.List(ctx, owner, messagedomain.Page{})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if ownerList.Total != 1 || ownerList.Unread != 1 {
		t.Fatalf("owner: want 1 total and 1 unread, got %d/%d", ownerList.Total, ownerList.Unread)
	}

	otherList, err := f.notifications.List(ctx, other, messagedomain.Page{})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if otherList.Total != 0 {
		t.Fatalf("another account can see it: %d notifications", otherList.Total)
	}

	// And cannot mark it read, even holding the id.
	id := ownerList.Notifications[0].ID
	markErr := f.notifications.MarkRead(ctx, other, id)
	if markErr == nil {
		t.Fatal("another account marked it read")
	}
	// The same answer as a notification that does not exist: distinguishing
	// them would confirm the id is real.
	if !apperrors.IsType(markErr, apperrors.TypeNotFound) {
		t.Fatalf("want NOT_FOUND, got %v", markErr)
	}

	missing := f.notifications.MarkRead(ctx, other, uuid.New())
	if !apperrors.IsType(missing, apperrors.TypeNotFound) {
		t.Fatalf("want NOT_FOUND for an id that does not exist, got %v", missing)
	}
}

func TestMessages_MarkReadAndMarkAllRead(t *testing.T) {
	f := newMessagesFixture(t)
	ctx := context.Background()
	owner := uuid.New()

	for i := 0; i < 3; i++ {
		if err := f.notifier.Notify(ctx, owner, messagedomain.KindInfo, "Message", "body"); err != nil {
			t.Fatalf("notifying: %v", err)
		}
	}

	count, err := f.notifications.UnreadCount(ctx, owner)
	if err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count.Unread != 3 {
		t.Fatalf("want 3 unread, got %d", count.Unread)
	}

	list, err := f.notifications.List(ctx, owner, messagedomain.Page{})
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if err := f.notifications.MarkRead(ctx, owner, list.Notifications[0].ID); err != nil {
		t.Fatalf("marking read: %v", err)
	}

	// Marking the same one again is NOT_FOUND: the conditional update matched
	// nothing, which is also what stops two requests from both "winning".
	if err := f.notifications.MarkRead(ctx, owner, list.Notifications[0].ID); err == nil {
		t.Fatal("marking an already-read notification succeeded twice")
	}

	remaining, err := f.notifications.MarkAllRead(ctx, owner)
	if err != nil {
		t.Fatalf("marking all read: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("want 2 remaining marked read, got %d", remaining)
	}

	count, err = f.notifications.UnreadCount(ctx, owner)
	if err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count.Unread != 0 {
		t.Fatalf("want 0 unread, got %d", count.Unread)
	}
}

func TestMessages_SendValidatesItsInput(t *testing.T) {
	f := newMessagesFixture(t)
	ctx := context.Background()

	cases := map[string]*messageservice.SendNotificationRequestDTO{
		"no recipient": {Title: "Hello"},
		"no title":     {UserID: uuid.New()},
		"bad kind":     {UserID: uuid.New(), Kind: "urgent", Title: "Hello"},
	}

	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := f.notifications.Send(ctx, req); err == nil {
				t.Fatal("accepted an invalid notification")
			}
		})
	}

	// An unspecified kind defaults rather than failing.
	ok, err := f.notifications.Send(ctx, &messageservice.SendNotificationRequestDTO{
		UserID: uuid.New(), Title: "Hello",
	})
	if err != nil {
		t.Fatalf("a notification with no kind was refused: %v", err)
	}
	if ok.Kind != messagedomain.KindInfo.String() {
		t.Fatalf("want the default kind, got %q", ok.Kind)
	}
}
