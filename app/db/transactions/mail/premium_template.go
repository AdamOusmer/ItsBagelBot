// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package mail

import (
	"html/template"
	"strings"

	"ItsBagelBot/internal/domain/i18n"
)

const (
	colorBlack = "#111113"
	colorCard  = "#18181b"
	colorInset = "#121214"
	colorRule  = "#2e2e32"
	colorFoot  = "#232325"
	colorBody  = "#b4b4bc"
	colorSoft  = "#c2c2c9"
	colorPaid  = "#eadbbd"
	colorTan   = "#d8bf91"
	colorTanLt = "#eadbbd"
	colorWhite = "#f4f4f5"
	colorMuted = "#a1a1aa"
	colorGreen = "#62a593"

	fontDisplay = "'Syne','Avenir Next','Helvetica Neue',Arial,sans-serif"
	fontBody    = "'DM Sans','Helvetica Neue',Arial,sans-serif"
	fontMono    = "'DM Mono',Menlo,Consolas,monospace"

	legacyLogoURL = "https://itsbagelbot.com/logo.png"
	logoCID       = "itsbagelbot-logo-v1"
	logoSrc       = "cid:" + logoCID
	supportURL    = "https://itsbagelbot.com"
)

var premiumTmpl = template.Must(template.New("premium").Parse(strings.TrimSpace(`
<!DOCTYPE html>
<html lang="{{.Locale}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="dark">
<meta name="supported-color-schemes" content="dark">
<style>
  @import url('https://fonts.googleapis.com/css2?family=Syne:wght@600;800&family=DM+Sans:wght@400;500;600&family=DM+Mono:wght@400;500&family=Caveat:wght@600&display=swap');
</style>
<title>{{.Title}}</title>
</head>
<body style="margin:0;padding:0;background-color:` + colorBlack + `;">
<!-- preheader: inbox preview line, invisible in the body -->
<div style="display:none;max-height:0;overflow:hidden;mso-hide:all;">{{.Preheader}}</div>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" bgcolor="` + colorBlack + `" style="background-color:` + colorBlack + `;">
<tr><td align="center" style="padding:40px 16px 48px;">

  <table role="presentation" width="560" cellpadding="0" cellspacing="0" border="0" style="width:560px;max-width:100%;">

    <!-- logo + wordmark, centered -->
    <tr><td align="center" style="padding:0 0 24px;">
      <img src="` + logoSrc + `" width="52" height="52" alt="ItsBagelBot" style="display:inline-block;width:52px;height:52px;vertical-align:middle;">
      <span style="font-family:` + fontDisplay + `;font-size:20px;font-weight:800;letter-spacing:-0.01em;color:` + colorWhite + `;vertical-align:middle;padding-left:12px;">ItsBagelBot</span>
    </td></tr>

    <!-- card -->
    <tr><td bgcolor="` + colorCard + `" style="background-color:` + colorCard + `;border-radius:16px;overflow:hidden;">
      <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">
        <!-- green to gold line -->
        <tr><td height="3" bgcolor="#62a593" style="height:3px;background-color:#62a593;background-image:linear-gradient(90deg,#62a593,#d8bf91);font-size:0;line-height:0;">&nbsp;</td></tr>

        <tr><td style="padding:40px 40px 36px;">

          <div style="font-family:` + fontMono + `;font-size:12px;line-height:1.4;letter-spacing:0.18em;text-transform:uppercase;color:` + colorTan + `;padding-bottom:16px;">
            Premium &middot; {{.Category}}
          </div>

          <div style="font-family:` + fontDisplay + `;font-size:28px;line-height:1.15;font-weight:600;letter-spacing:-0.02em;color:` + colorWhite + `;padding-bottom:16px;">
            {{.Heading}}
          </div>

{{if .Giveaway}}
          <div style="font-family:` + fontBody + `;font-size:16px;line-height:1.7;color:` + colorBody + `;padding-bottom:20px;">
            {{.GiveawayIntro}} <strong style="color:` + colorTanLt + `;font-weight:600;">{{.ProductPeriod}}</strong>.
          </div>
          <div style="font-family:` + fontBody + `;font-size:16px;line-height:1.7;color:` + colorSoft + `;padding-bottom:18px;">{{.Giveaway.PeriodText}}</div>
{{if .Giveaway.Subscriber}}
          <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 24px;">
            <tr><td bgcolor="` + colorInset + `" style="background-color:` + colorInset + `;border-radius:8px;padding:16px 18px;">
              <div style="font-family:` + fontMono + `;font-size:11px;font-weight:500;letter-spacing:0.16em;text-transform:uppercase;color:` + colorTan + `;padding-bottom:8px;">{{.SubscriptionLabel}}</div>
              <div style="font-family:` + fontBody + `;font-size:14px;line-height:1.65;color:` + colorBody + `;">{{.Giveaway.Situation}}</div>
            </td></tr>
          </table>
{{else if .Giveaway.Situation}}
          <div style="font-family:` + fontBody + `;font-size:14px;line-height:1.65;color:` + colorBody + `;padding-bottom:24px;">{{.Giveaway.Situation}}</div>
{{end}}
{{else}}
          <div style="font-family:` + fontBody + `;font-size:16px;line-height:1.7;color:` + colorBody + `;padding-bottom:20px;">
            {{if .Gift.Gifter}}<strong style="color:` + colorTanLt + `;font-weight:600;">{{.Gift.Gifter}}</strong> {{.GiftIntro}}{{else}}{{.GiftIntro}}{{end}}
            <strong style="color:` + colorTanLt + `;font-weight:600;">{{.ProductPeriod}}</strong>.
            {{.GiftNoStrings}}
          </div>
{{if .Gift.Message}}
          <!-- personal note from the buyer -->
          <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 24px;">
            <tr><td bgcolor="` + colorInset + `" style="background-color:` + colorInset + `;border-radius:8px;padding:16px 18px;">
              <div style="font-family:` + fontMono + `;font-size:11px;font-weight:500;letter-spacing:0.16em;text-transform:uppercase;color:` + colorTan + `;padding-bottom:8px;">{{.Gift.NoteLabel}}</div>
              <div style="font-family:` + fontBody + `;font-size:15px;line-height:1.6;color:` + colorSoft + `;font-style:italic;">&ldquo;{{.Gift.Message}}&rdquo;</div>
            </td></tr>
          </table>
{{end}}
{{end}}
          <!-- fulfillment status -->
          <div style="padding-bottom:26px;">
            <div style="font-family:` + fontMono + `;font-size:12px;line-height:1.6;letter-spacing:0.04em;color:{{.StatusColor}};">{{.StatusLine}}</div>
{{if .StatusDetail}}
            <div style="font-family:` + fontBody + `;font-size:14px;line-height:1.6;color:` + colorBody + `;padding-top:7px;">{{.StatusDetail}}</div>
{{end}}
          </div>

          <!-- divider -->
          <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">
            <tr><td height="1" bgcolor="` + colorRule + `" style="height:1px;background-color:` + colorRule + `;font-size:0;line-height:0;">&nbsp;</td></tr>
          </table>

          <!-- perks -->
          <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="padding:0;">
            <tr>
              <td width="18" valign="top" style="padding:22px 0 0;font-family:` + fontBody + `;font-size:14px;color:` + colorTan + `;">&#8226;</td>
              <td style="padding:22px 0 0;font-family:` + fontBody + `;font-size:14px;line-height:1.6;color:` + colorMuted + `;">
                <span style="color:` + colorWhite + `;font-weight:600;">{{.PriorityLabel}}</span> {{.PriorityBody}}
              </td>
            </tr>
            <tr>
              <td width="18" valign="top" style="padding:10px 0 0;font-family:` + fontBody + `;font-size:14px;color:` + colorTan + `;">&#8226;</td>
              <td style="padding:10px 0 0;font-family:` + fontBody + `;font-size:14px;line-height:1.6;color:` + colorMuted + `;">
                <span style="color:` + colorWhite + `;font-weight:600;">{{.PerksLabel}}</span> {{.PerkPeriod}}
              </td>
            </tr>
            <tr>
              <td width="18" valign="top" style="padding:10px 0 0;font-family:` + fontBody + `;font-size:14px;color:` + colorTan + `;">&#8226;</td>
              <td style="padding:10px 0 0;font-family:` + fontBody + `;font-size:14px;line-height:1.6;color:` + colorMuted + `;">
                <span style="color:` + colorWhite + `;font-weight:600;">{{.BetaLabel}}</span> {{.BetaBody}}
              </td>
            </tr>
          </table>

          <!-- CTA -->
          <table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin-top:30px;">
            <tr><td bgcolor="` + colorWhite + `" style="background-color:` + colorWhite + `;border-radius:8px;">
              <a href="{{.DashboardURL}}" style="display:inline-block;padding:13px 20px;font-family:` + fontBody + `;font-size:14px;font-weight:500;line-height:1;letter-spacing:-0.005em;color:` + colorBlack + `;text-decoration:none;border-radius:8px;">
                {{.ActionLabel}} &rarr;
              </a>
            </td></tr>
          </table>

          <!-- sign-off -->
          <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin-top:36px;">
            <tr>
              <td valign="middle">
                <span style="display:inline-block;">
                  <span style="font-family:'Caveat','Segoe Script','Bradley Hand','Snell Roundhand',cursive;font-size:26px;font-weight:600;line-height:1;color:` + colorTanLt + `;">{{.Signature}}</span>
                </span>
              </td>
            </tr>
          </table>

        </td></tr>
      </table>
    </td></tr>

    <!-- footer -->
    <tr><td align="center" style="padding:26px 20px 0;font-family:` + fontBody + `;font-size:12px;line-height:1.7;color:` + colorMuted + `;">
      <div style="border-top:1px solid ` + colorFoot + `;padding-top:16px;padding-bottom:18px;">
        <strong style="color:` + colorTanLt + `;font-weight:600;">{{.SafetyLabel}}</strong>
        {{.SafetyBody}}
        <a href="` + supportURL + `" style="color:` + colorTanLt + `;text-decoration:none;">itsbagelbot.com</a>.
      </div>
      {{.Footer}}<br>
      <a href="https://itsbagelbot.com" style="color:` + colorTanLt + `;text-decoration:none;">itsbagelbot.com</a>
    </td></tr>

  </table>

</td></tr>
</table>
</body>
</html>
`)))

type premiumData struct {
	Title, Preheader, Category, Heading               string
	StatusLine, StatusDetail, StatusColor, PerkPeriod string
	ActionLabel, Footer, DashboardURL                 string
	Gift                                              *giftData
	Giveaway                                          *giveawayData
	Locale                                            string
	GiftIntro, GiftNoStrings                          string
	PriorityLabel, PriorityBody                       string
	PerksLabel                                        string
	BetaLabel, BetaBody                               string
	SubscriptionLabel, SafetyLabel, SafetyBody        string
	GiveawayIntro, Signature                          string
	ProductPeriod                                     string
}

func mailChrome(locale string) map[string]string {
	keys := []string{"mail.gift.intro", "mail.gift.no_strings", "mail.priority.label", "mail.priority.body", "mail.perks.label", "mail.beta.label", "mail.beta.body", "mail.subscription", "mail.safety.label", "mail.safety.body"}
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = i18n.T(locale, key)
	}
	return out
}

func renderPremium(data premiumData) (string, error) {
	if !i18n.Supported(data.Locale) {
		data.Locale = i18n.DefaultLocale
	}
	var body strings.Builder
	if err := premiumTmpl.Execute(&body, data); err != nil {
		return "", err
	}
	return body.String(), nil
}
