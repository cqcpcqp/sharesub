package tencentmail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	ses "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ses/v20201002"
)

const verificationSubject = "验证你的 ShareSub 邮箱"

type Config struct {
	SecretID   string
	SecretKey  string
	Region     string
	FromEmail  string
	FromName   string
	TemplateID uint64
}

type emailClient interface {
	SendEmailWithContext(context.Context, *ses.SendEmailRequest) (*ses.SendEmailResponse, error)
}

type Sender struct {
	client      emailClient
	fromAddress string
	templateID  uint64
}

func New(config Config) (*Sender, error) {
	if strings.TrimSpace(config.SecretID) == "" || strings.TrimSpace(config.SecretKey) == "" || strings.TrimSpace(config.Region) == "" || config.TemplateID == 0 {
		return nil, errors.New("Tencent SES credentials, region, and template ID are required")
	}
	fromEmail := strings.TrimSpace(config.FromEmail)
	parsed, err := mail.ParseAddress(fromEmail)
	if err != nil || parsed.Address != fromEmail {
		return nil, errors.New("Tencent SES sender email is invalid")
	}
	fromName := strings.TrimSpace(config.FromName)
	if fromName == "" || strings.Contains(fromName, ":") || strings.ContainsAny(fromName, "\r\n") {
		return nil, errors.New("Tencent SES sender name is invalid")
	}
	clientProfile := profile.NewClientProfile()
	clientProfile.HttpProfile.ReqTimeout = 15
	client, err := ses.NewClient(common.NewCredential(config.SecretID, config.SecretKey), config.Region, clientProfile)
	if err != nil {
		return nil, fmt.Errorf("create Tencent SES client: %w", err)
	}
	return &Sender{client: client, fromAddress: fmt.Sprintf("%s <%s>", fromName, fromEmail), templateID: config.TemplateID}, nil
}

func (s *Sender) SendEmailVerification(ctx context.Context, recipient, token string) error {
	recipient = strings.TrimSpace(recipient)
	parsed, err := mail.ParseAddress(recipient)
	if err != nil || parsed.Address != recipient {
		return errors.New("verification recipient is invalid")
	}
	if strings.TrimSpace(token) == "" {
		return errors.New("verification token is required")
	}
	templateDataBytes, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		return fmt.Errorf("encode Tencent SES template data: %w", err)
	}
	templateData := string(templateDataBytes)
	request := ses.NewSendEmailRequest()
	request.FromEmailAddress = common.StringPtr(s.fromAddress)
	request.Subject = common.StringPtr(verificationSubject)
	request.Destination = []*string{common.StringPtr(recipient)}
	request.Template = &ses.Template{TemplateID: common.Uint64Ptr(s.templateID), TemplateData: common.StringPtr(templateData)}
	request.TriggerType = common.Uint64Ptr(1)
	request.Unsubscribe = common.StringPtr("0")
	response, err := s.client.SendEmailWithContext(ctx, request)
	if err != nil {
		return fmt.Errorf("Tencent SES SendEmail: %w", err)
	}
	if response == nil || response.Response == nil || response.Response.MessageId == nil || *response.Response.MessageId == "" {
		return errors.New("Tencent SES SendEmail returned no message ID")
	}
	return nil
}
