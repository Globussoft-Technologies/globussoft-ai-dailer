// Package email sends transactional emails via SMTP (Gmail/STARTTLS).
package email

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"html/template"
	"net"
	"net/smtp"
	"strings"

	"go.uber.org/zap"
)

// Service sends transactional emails.
type Service struct {
	host     string
	port     int
	user     string
	password string
	fromName string
	appURL   string
	log      *zap.Logger
}

// New creates an email Service.
func New(host string, port int, user, password, fromName, appURL string, log *zap.Logger) *Service {
	return &Service{
		host:     host,
		port:     port,
		user:     user,
		password: password,
		fromName: fromName,
		appURL:   appURL,
		log:      log,
	}
}

// Send sends a plain HTML email.
func (s *Service) Send(to, subject, htmlBody string) error {
	if s.user == "" || s.password == "" {
		s.log.Warn("email: SMTP not configured, skipping send",
			zap.String("to", to), zap.String("subject", subject))
		return nil
	}

	from := fmt.Sprintf("%s <%s>", s.fromName, s.user)
	msg := buildMIME(from, to, subject, htmlBody)

	addr := net.JoinHostPort(s.host, fmt.Sprintf("%d", s.port))
	auth := smtp.PlainAuth("", s.user, s.password, s.host)

	// Dial manually for STARTTLS support
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("email dial: %w", err)
	}
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("email smtp client: %w", err)
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsCfg := &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("email starttls: %w", err)
		}
	}

	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("email auth: %w", err)
	}
	if err := client.Mail(s.user); err != nil {
		return fmt.Errorf("email from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("email rcpt: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("email data: %w", err)
	}
	defer w.Close()
	_, err = w.Write(msg)
	return err
}

// SendWelcome sends a welcome email to a new user.
func (s *Service) SendWelcome(to, name string) error {
	body := renderTemplate(welcomeTmpl, map[string]string{
		"Name":   name,
		"AppURL": s.appURL,
	})
	return s.Send(to, "Welcome to Callified AI!", body)
}

// SendPaymentReceipt sends a payment confirmation email.
func (s *Service) SendPaymentReceipt(to, orgName, planName, paymentID string, amount float64) error {
	body := renderTemplate(receiptTmpl, map[string]any{
		"OrgName":   orgName,
		"PlanName":  planName,
		"PaymentID": paymentID,
		"Amount":    fmt.Sprintf("₹%.2f", amount),
		"AppURL":    s.appURL,
	})
	return s.Send(to, "Payment Receipt — Callified AI", body)
}

// SendAppointmentConfirmation sends a WA/email appointment confirmation.
func (s *Service) SendAppointmentConfirmation(to, leadName, appointmentDate, agentName string) error {
	body := renderTemplate(appointmentTmpl, map[string]string{
		"LeadName":        leadName,
		"AppointmentDate": appointmentDate,
		"AgentName":       agentName,
		"AppURL":          s.appURL,
	})
	return s.Send(to, "Your Appointment is Confirmed", body)
}

// SendCampaignSummary sends a campaign completion summary.
func (s *Service) SendCampaignSummary(to, campaignName string, totalCalls, connected, appointments int) error {
	body := renderTemplate(campaignSummaryTmpl, map[string]any{
		"CampaignName": campaignName,
		"TotalCalls":   totalCalls,
		"Connected":    connected,
		"Appointments": appointments,
		"AppURL":       s.appURL,
	})
	return s.Send(to, fmt.Sprintf("Campaign Summary: %s", campaignName), body)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func buildMIME(from, to, subject string, htmlBody string) []byte {
	var buf bytes.Buffer
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	buf.WriteString(fmt.Sprintf("From: %s\r\n", from))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", to))
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	buf.WriteString("\r\n")
	buf.WriteString(htmlBody)
	return buf.Bytes()
}

func renderTemplate(tmplStr string, data any) string {
	t, err := template.New("email").Parse(tmplStr)
	if err != nil {
		return strings.ReplaceAll(tmplStr, "{{", "")
	}
	var buf bytes.Buffer
	_ = t.Execute(&buf, data)
	return buf.String()
}

// ── Email Templates ───────────────────────────────────────────────────────────

const welcomeTmpl = `<!DOCTYPE html><html><body style="font-family:sans-serif;max-width:600px;margin:0 auto;">
<h2>Welcome to Callified AI, {{.Name}}!</h2>
<p>Your account is ready. Start making AI-powered sales calls in minutes.</p>
<p><a href="{{.AppURL}}" style="background:#6366f1;color:#fff;padding:10px 20px;border-radius:6px;text-decoration:none;">Open Dashboard</a></p>
<p style="color:#888;font-size:12px;">Callified AI — AI Sales Dialer</p>
</body></html>`

const receiptTmpl = `<!DOCTYPE html><html><body style="font-family:sans-serif;max-width:600px;margin:0 auto;">
<h2>Payment Confirmed</h2>
<p>Hi {{.OrgName}}, your payment has been received.</p>
<table style="width:100%;border-collapse:collapse;">
<tr><td style="padding:8px;border:1px solid #eee;"><b>Plan</b></td><td style="padding:8px;border:1px solid #eee;">{{.PlanName}}</td></tr>
<tr><td style="padding:8px;border:1px solid #eee;"><b>Amount</b></td><td style="padding:8px;border:1px solid #eee;">{{.Amount}}</td></tr>
<tr><td style="padding:8px;border:1px solid #eee;"><b>Payment ID</b></td><td style="padding:8px;border:1px solid #eee;">{{.PaymentID}}</td></tr>
</table>
<p><a href="{{.AppURL}}/billing">View Invoices</a></p>
</body></html>`

const appointmentTmpl = `<!DOCTYPE html><html><body style="font-family:sans-serif;max-width:600px;margin:0 auto;">
<h2>Appointment Confirmed</h2>
<p>Hi {{.LeadName}}, your appointment has been scheduled.</p>
<p><b>Date/Time:</b> {{.AppointmentDate}}</p>
<p><b>With:</b> {{.AgentName}}</p>
</body></html>`

const campaignSummaryTmpl = `<!DOCTYPE html><html><body style="font-family:sans-serif;max-width:600px;margin:0 auto;">
<h2>Campaign Summary: {{.CampaignName}}</h2>
<table style="width:100%;border-collapse:collapse;">
<tr><td style="padding:8px;border:1px solid #eee;"><b>Total Calls</b></td><td style="padding:8px;border:1px solid #eee;">{{.TotalCalls}}</td></tr>
<tr><td style="padding:8px;border:1px solid #eee;"><b>Connected</b></td><td style="padding:8px;border:1px solid #eee;">{{.Connected}}</td></tr>
<tr><td style="padding:8px;border:1px solid #eee;"><b>Appointments Set</b></td><td style="padding:8px;border:1px solid #eee;">{{.Appointments}}</td></tr>
</table>
<p><a href="{{.AppURL}}">View Full Report</a></p>
</body></html>`
