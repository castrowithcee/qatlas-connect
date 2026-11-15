package github

import "github.com/castrowithcee/qatlas-connect/internal/capability"

// Expected tools of the "notifications" group: ID, output sample and risk contract, one tool per line in sorted order.
func init() {
	addExpectedTools("notifications", []expectedTool{
		{id: "github.notifications.dismiss", output: NotificationDismissed{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyUnknown)},
		{id: "github.notifications.get", output: Notification{}, risk: readRisk},
		{id: "github.notifications.list", output: NotificationList{}, risk: readRisk},
		{id: "github.notifications.mark-all", allow: true, output: NotificationsMarked{}, risk: guardedRisk(capability.EffectUpdate, capability.IdempotencyUnknown, notificationSensitivity)},
		{id: "github.thread-subscriptions.set", output: ThreadSubscription{}, risk: changeRisk(capability.EffectUpdate, capability.IdempotencyUnknown)},
	})
}
