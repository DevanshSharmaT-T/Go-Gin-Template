// File: internal/modules/messages/module.go

package messages

import (
	"go.uber.org/fx"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/messages/api"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/messages/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/messages/infra"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/messages/service"
)

// Module wires the messages module: in-app notifications and the record of
// every email the application has tried to send.
var Module = fx.Module("messages",
	fx.Provide(
		infra.NewGormNotificationRepository,
		infra.NewGormOutboundMailRepository,

		service.NewNotificationService,
		api.NewNotificationHandler,

		// The Notifier port, so another module can raise a notification without
		// depending on this module's service. The service itself implements it;
		// this line is what binds it under the interface.
		func(s *service.NotificationService) domain.Notifier { return s },

		fx.Annotate(
			func() any { return &domain.Notification{} },
			fx.ResultTags(`group:"models"`),
		),
		fx.Annotate(
			func() any { return &domain.OutboundMail{} },
			fx.ResultTags(`group:"models"`),
		),
	),
)

// RecordMail makes every mail.Mailer in the application the recording one.
//
// **It is separate from Module, and it has to be applied at the root scope.**
// fx.Decorate is scoped: a decoration declared inside an fx.Module applies to
// that module and its descendants only. Put this inside Module above and the
// auth module — the only thing that actually sends mail — would keep the
// undecorated mailer, nothing would be recorded, and there would be no error
// anywhere to say so. Just an empty table.
//
// internal/app includes it as a sibling of Module, and internal/app is an
// fx.Options rather than an fx.Module, so "root" is where it lands.
var RecordMail = fx.Decorate(service.NewRecordingMailer)
