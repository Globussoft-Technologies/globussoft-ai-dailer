package billing

import (
	"bytes"
	"fmt"
	"html/template"
)

const invoiceTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>Invoice {{.InvoiceNumber}}</title>
<style>
  body{font-family:sans-serif;max-width:700px;margin:40px auto;color:#222}
  .header{display:flex;justify-content:space-between;align-items:flex-start}
  h1{color:#6366f1;margin:0}
  .meta{text-align:right;font-size:14px;color:#555}
  table{width:100%;border-collapse:collapse;margin-top:24px}
  th{background:#f5f5f5;padding:10px;text-align:left;font-size:13px;border:1px solid #ddd}
  td{padding:10px;border:1px solid #ddd;font-size:13px}
  .total{font-size:18px;font-weight:bold;text-align:right;margin-top:16px}
  .footer{margin-top:40px;font-size:12px;color:#888;text-align:center}
</style>
</head>
<body>
<div class="header">
  <div>
    <h1>Callified AI</h1>
    <p style="margin:4px 0;color:#555">AI-Powered Sales Dialer</p>
  </div>
  <div class="meta">
    <div><b>Invoice #{{.InvoiceNumber}}</b></div>
    <div>Date: {{.Date}}</div>
    <div>Payment ID: {{.PaymentID}}</div>
  </div>
</div>
<hr style="margin:24px 0;border-color:#eee">
<p><b>Billed to:</b> {{.OrgName}}</p>
<table>
  <tr><th>Description</th><th>Amount</th></tr>
  <tr>
    <td>{{.PlanName}} Subscription</td>
    <td>₹{{.Amount}}</td>
  </tr>
</table>
<p class="total">Total: ₹{{.Amount}}</p>
<div class="footer">
  Callified AI — Thank you for your business.<br>
  This is a computer-generated invoice and does not require a signature.
</div>
</body>
</html>`

// GenerateInvoiceHTML renders an invoice as an HTML string.
func GenerateInvoiceHTML(orgName, planName, amount, paymentID, date, invoiceNumber string) string {
	t, err := template.New("invoice").Parse(invoiceTmpl)
	if err != nil {
		return fmt.Sprintf("<p>Invoice %s — %s — %s</p>", invoiceNumber, planName, amount)
	}
	var buf bytes.Buffer
	_ = t.Execute(&buf, map[string]string{
		"OrgName":       orgName,
		"PlanName":      planName,
		"Amount":        amount,
		"PaymentID":     paymentID,
		"Date":          date,
		"InvoiceNumber": invoiceNumber,
	})
	return buf.String()
}
