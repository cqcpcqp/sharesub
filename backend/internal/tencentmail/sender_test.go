package tencentmail

import (
	"context"
	"testing"

	ses "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ses/v20201002"
)

type recordingClient struct {
	request *ses.SendEmailRequest
	err     error
}

func (c *recordingClient) SendEmailWithContext(_ context.Context, request *ses.SendEmailRequest) (*ses.SendEmailResponse, error) {
	c.request = request
	if c.err != nil {
		return nil, c.err
	}
	response := ses.NewSendEmailResponse()
	messageID := "message-id"
	response.Response = &ses.SendEmailResponseParams{MessageId: &messageID}
	return response, nil
}

func TestSendEmailVerificationUsesApprovedTemplate(t *testing.T) {
	client := &recordingClient{}
	sender := &Sender{client: client, fromAddress: "ShareSub <no-reply@notify.underelay.com>", templateID: 212354}
	if err := sender.SendEmailVerification(context.Background(), "member@example.com", "ss_verify_secret"); err != nil {
		t.Fatal(err)
	}
	request := client.request
	if request == nil || request.FromEmailAddress == nil || *request.FromEmailAddress != sender.fromAddress || request.Subject == nil || *request.Subject != verificationSubject {
		t.Fatalf("request identity = %+v", request)
	}
	if len(request.Destination) != 1 || *request.Destination[0] != "member@example.com" {
		t.Fatalf("destinations = %+v", request.Destination)
	}
	if request.Template == nil || request.Template.TemplateID == nil || *request.Template.TemplateID != 212354 || request.Template.TemplateData == nil || *request.Template.TemplateData != `{"token":"ss_verify_secret"}` {
		t.Fatalf("template = %+v", request.Template)
	}
	if request.TriggerType == nil || *request.TriggerType != 1 || request.Unsubscribe == nil || *request.Unsubscribe != "0" {
		t.Fatalf("delivery flags = trigger %v unsubscribe %v", request.TriggerType, request.Unsubscribe)
	}
}

func TestSendEmailVerificationRejectsInvalidRecipient(t *testing.T) {
	sender := &Sender{client: &recordingClient{}, fromAddress: "ShareSub <no-reply@notify.underelay.com>", templateID: 212354}
	if err := sender.SendEmailVerification(context.Background(), "not-an-email", "ss_verify_secret"); err == nil {
		t.Fatal("expected invalid recipient error")
	}
}
