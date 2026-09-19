package microsoft365

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
)

type graphSendMailRequest struct {
	Message graphMessage `json:"message"`
}

type graphMessage struct {
	Subject      string           `json:"subject"`
	Body         graphItemBody    `json:"body"`
	ToRecipients []graphRecipient `json:"toRecipients"`
}

type graphItemBody struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

type graphRecipient struct {
	EmailAddress graphEmailAddress `json:"emailAddress"`
}

type graphEmailAddress struct {
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
}

func mapMessage(msg email.Message) ([]byte, error) {
	addr := strings.TrimSpace(msg.To.Address)
	rec := graphRecipient{
		EmailAddress: graphEmailAddress{Address: addr},
	}
	if name := strings.TrimSpace(msg.To.DisplayName); name != "" {
		rec.EmailAddress.Name = name
	}

	body := graphItemBody{}
	if strings.TrimSpace(msg.HTMLBody) != "" {
		body.ContentType = "HTML"
		body.Content = msg.HTMLBody
	} else {
		body.ContentType = "Text"
		body.Content = msg.TextBody
	}

	payload := graphSendMailRequest{
		Message: graphMessage{
			Subject:      msg.Subject,
			Body:         body,
			ToRecipients: []graphRecipient{rec},
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, email.Permanent(fmt.Errorf("graph payload marshal failed"))
	}
	return b, nil
}

type graphErrorEnvelope struct {
	Error *graphErrorBody `json:"error"`
}

type graphErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type oauthErrorBody struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}
