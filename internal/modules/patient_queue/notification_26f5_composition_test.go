package patient_queue

import "testing"

func TestServiceChannelsFromNotificationEmailFlag(t *testing.T) {
	t.Parallel()

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()
		s := NewService(nil).WithNotificationLifecycleConfig(NotificationLifecycleConfig{EmailEnabled: false})
		ch := s.NotificationLifecycleChannels()
		if len(ch) != 1 || ch[0] != NotifChannelLog {
			t.Fatalf("channels=%v want [LOG]", ch)
		}
	})

	t.Run("enabled", func(t *testing.T) {
		t.Parallel()
		s := NewService(nil).WithNotificationLifecycleConfig(NotificationLifecycleConfig{EmailEnabled: true})
		ch := s.NotificationLifecycleChannels()
		if len(ch) != 2 || ch[0] != NotifChannelLog || ch[1] != NotifChannelEmail {
			t.Fatalf("channels=%v want [LOG EMAIL]", ch)
		}
	})
}
