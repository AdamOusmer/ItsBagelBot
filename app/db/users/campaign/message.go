// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package campaign

import (
	"fmt"
	"html"
	"strconv"
	"strings"

	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/domain/consent"
	"ItsBagelBot/pkg/email"
)

type footerCopy struct {
	reason      string
	unsubscribe string
	preferences string
	questions   string
}

var footerCopies = map[string]footerCopy{
	"en": {
		reason:      "You are getting this because you chose to receive product news from ItsBagelBot.",
		unsubscribe: "Unsubscribe",
		preferences: "Email preferences",
		questions:   "Questions? Write to",
	},
	"fr": {
		reason:      "Tu reçois ce message parce que tu as choisi de recevoir les nouveautés d'ItsBagelBot.",
		unsubscribe: "Se désabonner",
		preferences: "Préférences e-mail",
		questions:   "Une question ? Écris à",
	},
	"es": {
		reason:      "Recibes este mensaje porque elegiste recibir novedades de ItsBagelBot.",
		unsubscribe: "Cancelar suscripción",
		preferences: "Preferencias de correo",
		questions:   "¿Preguntas? Escribe a",
	},
	"pt-br": {
		reason:      "Você recebe esta mensagem porque escolheu receber novidades do ItsBagelBot.",
		unsubscribe: "Cancelar inscrição",
		preferences: "Preferências de e-mail",
		questions:   "Dúvidas? Escreva para",
	},
	"ru": {
		reason:      "Вы получаете это письмо, потому что выбрали новости ItsBagelBot.",
		unsubscribe: "Отписаться",
		preferences: "Настройки почты",
		questions:   "Вопросы? Пишите на",
	},
	"de": {
		reason:      "Du erhältst diese Nachricht, weil du Neuigkeiten von ItsBagelBot erhalten möchtest.",
		unsubscribe: "Abmelden",
		preferences: "E-Mail-Einstellungen",
		questions:   "Fragen? Schreib an",
	},
}

func footerFor(locale string) footerCopy {
	if copy, ok := footerCopies[locale]; ok {
		return copy
	}
	return footerCopies[consent.DefaultCampaignLocale]
}

type rendering struct {
	recipient repository.MarketingRecipient
	content   consent.CampaignContent
	token     string
}

func (s *Service) message(campaignID int, r rendering, key string) email.Message {
	link := s.settings.Origin + "/u/" + r.token
	footer := footerFor(r.recipient.Locale)
	return email.Message{
		From:    s.settings.From,
		To:      r.recipient.Email,
		Subject: r.content.Subject,
		HTML:    s.htmlBody(r, footer, link),
		Text:    s.textBody(r, footer, link),
		Headers: map[string]string{
			"List-Unsubscribe":      "<" + link + ">",
			"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
		},
		Tags:           map[string]string{"category": "marketing", "campaign": strconv.Itoa(campaignID)},
		IdempotencyKey: key,
	}
}

func (s *Service) htmlBody(r rendering, footer footerCopy, link string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<!DOCTYPE html><html lang="%s"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title></head>`,
		html.EscapeString(r.recipient.Locale), html.EscapeString(r.content.Subject))
	b.WriteString(`<body style="margin:0;padding:0;background-color:#ffffff;"><table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr><td align="center" style="padding:32px 16px;">`)
	b.WriteString(`<table role="presentation" width="560" cellpadding="0" cellspacing="0" border="0" style="width:560px;max-width:100%;font-family:'DM Sans','Helvetica Neue',Arial,sans-serif;font-size:16px;line-height:1.55;color:#18181b;"><tr><td>`)
	b.WriteString(r.content.HTML)
	b.WriteString(`</td></tr><tr><td style="padding-top:32px;border-top:1px solid #e4e4e7;font-size:13px;line-height:1.5;color:#52525b;">`)
	fmt.Fprintf(&b, `<p style="margin:24px 0 8px;">%s</p>`, html.EscapeString(footer.reason))
	fmt.Fprintf(&b, `<p style="margin:0 0 8px;">%s, %s</p>`, html.EscapeString(s.settings.SenderName), html.EscapeString(s.settings.MailingAddress))
	fmt.Fprintf(&b, `<p style="margin:0 0 8px;"><a href="%[1]s" style="color:#52525b;">%[2]s</a> | <a href="%[1]s" style="color:#52525b;">%[3]s</a></p>`,
		html.EscapeString(link), html.EscapeString(footer.unsubscribe), html.EscapeString(footer.preferences))
	fmt.Fprintf(&b, `<p style="margin:0;">%s <a href="mailto:%[2]s" style="color:#52525b;">%[2]s</a></p>`,
		html.EscapeString(footer.questions), html.EscapeString(s.settings.SupportEmail))
	b.WriteString("</td></tr></table></td></tr></table></body></html>")
	return b.String()
}

func (s *Service) textBody(r rendering, footer footerCopy, link string) string {
	return strings.Join([]string{
		strings.TrimSpace(r.content.Text),
		"",
		"--",
		footer.reason,
		s.settings.SenderName + ", " + s.settings.MailingAddress,
		footer.unsubscribe + ": " + link,
		footer.preferences + ": " + link,
		footer.questions + " " + s.settings.SupportEmail,
	}, "\n")
}
