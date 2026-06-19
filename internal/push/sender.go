package push

import (
	"context"
	"log/slog"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Sender delivers Web Push notifications using VAPID authentication.
type Sender struct {
	public  string
	private string
	subject string
}

// NewSender creates a Sender with the given VAPID credentials.
func NewSender(public, private, subject string) *Sender {
	return &Sender{public: public, private: private, subject: subject}
}

// Send delivers payloadJSON to a single subscription. If the push service
// returns 404 or 410 the subscription is removed from store. All other
// failures are logged but not propagated (best-effort delivery).
func (s *Sender) Send(ctx context.Context, store *Store, sub webpush.Subscription, payloadJSON []byte) {
	resp, err := webpush.SendNotificationWithContext(ctx, payloadJSON, &sub, &webpush.Options{
		VAPIDPublicKey:  s.public,
		VAPIDPrivateKey: s.private,
		Subscriber:      s.subject,
		TTL:             30,
	})
	if err != nil {
		slog.Warn("push send error", "endpoint", sub.Endpoint, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		slog.Info("push subscription expired; removing", "endpoint", sub.Endpoint, "status", resp.StatusCode)
		store.Remove(sub.Endpoint)
		return
	}
	if resp.StatusCode >= 300 {
		slog.Warn("push delivery failed", "endpoint", sub.Endpoint, "status", resp.StatusCode)
	}
}
