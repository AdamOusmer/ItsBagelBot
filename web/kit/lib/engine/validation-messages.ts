// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/** Translator shape intentionally stays smaller than the app i18n interface.
 * Server actions and client editors can both adapt their locale translator. */
export type ValidationTranslator = (
  key: string,
  params?: Record<string, string | number>
) => string;

/**
 * Localize a validator's stable English message at the display boundary.
 * Validation decisions and wire error strings remain unchanged. Dynamic names
 * and URL/path fragments are passed as parameters, so user input is preserved.
 * Unknown messages deliberately pass through for forward compatibility.
 */
export function translateValidationMessage(message: string | undefined, t: ValidationTranslator): string | undefined {
  if (!message) return message;
  let match: RegExpMatchArray | null;

  if ((match = message.match(/^Command name is required\.$/))) return t('validation.commandNameRequired');
  if ((match = message.match(/^Command name must be at most (\d+) characters\.$/))) return t('validation.commandNameMax', { max: match[1] });
  if ((match = message.match(/^Command name cannot contain spaces\.$/))) return t('validation.commandNameSpaces');
  if ((match = message.match(/^Command name only carries the "!" in chat\. Leave it out here\.$/))) return t('validation.commandNameBang');
  if ((match = message.match(/^Alternate name "([\s\S]*)" is required\.$/))) return t('validation.alternateNameRequired', { name: match[1] });
  if ((match = message.match(/^Alternate name "([\s\S]*)" must be at most (\d+) characters\.$/))) return t('validation.alternateNameMax', { name: match[1], max: match[2] });
  if ((match = message.match(/^Alternate name "([\s\S]*)" cannot contain spaces\.$/))) return t('validation.alternateNameSpaces', { name: match[1] });
  if ((match = message.match(/^Alternate name "([\s\S]*)" only carries the "!" in chat\. Leave it out here\.$/))) return t('validation.alternateNameBang', { name: match[1] });
  if ((match = message.match(/^"([\s\S]*)" is already the command's own name\.$/))) return t('validation.aliasOwnName', { name: match[1] });
  if ((match = message.match(/^"([\s\S]*)" is listed twice\.$/))) return t('validation.aliasDuplicate', { name: match[1] });
  if (message === 'Response is required.') return t('validation.responseRequired');
  if ((match = message.match(/^Response can be at most (\d+) lines\. Each line is sent as its own chat message\.$/))) return t('validation.responseLinesMax', { max: match[1] });
  if ((match = message.match(/^Each line must be at most (\d+) characters\.$/))) return t('validation.responseCharsMax', { max: match[1] });
  if (message === 'Response cannot contain control characters.') return t('validation.responseControlChars');
  if ((match = message.match(/^A response can reference at most (\d+) different fetched values \(\{urlfetch:…\}\)\.$/))) return t('validation.responseFetchMax', { max: match[1] });
  if ((match = message.match(/^Cooldown must be between 0 and (\d+) seconds\.$/))) return t('validation.cooldownRange', { max: match[1] });
  if (message === 'Cooldown must be a whole number of seconds.') return t('validation.cooldownInteger');
  if (message === 'User restriction must be a numeric Twitch user id.') return t('validation.userIdNumeric');
  if (message === 'Definition name is required.') return t('validation.definitionNameRequired');
  if ((match = message.match(/^Definition name must be at most (\d+) characters\.$/))) return t('validation.definitionNameMax', { max: match[1] });
  if (message === 'Use lower-case letters, digits and underscores only.') return t('validation.definitionNameCharset');
  if (message === 'URL is required.') return t('validation.urlRequired');
  if ((match = message.match(/^URL must be at most (\d+) characters\.$/))) return t('validation.urlMax', { max: match[1] });
  if (message === 'URL must start with https://') return t('validation.urlHttps');
  if (message === 'URL must point at a public https host.') return t('validation.urlPublic');
  if (message === 'Pick plain or json.') return t('validation.fetchKind');
  if (message === 'A plain fetch reads the whole body. Clear the path or switch to json.') return t('validation.plainPath');
  if ((match = message.match(/^Path can be at most (\d+) segments deep\.$/))) return t('validation.pathDepth', { max: match[1] });
  if ((match = message.match(/^"([\s\S]*)" cannot be used as a path segment: letters, digits, "-" and "_" only\.$/))) return t('validation.pathSegment', { segment: match[1] });
  if ((match = message.match(/^Key label must be at most (\d+) characters\.$/))) return t('validation.keyLabelMax', { max: match[1] });
  if ((match = message.match(/^A data source named "([\s\S]*)" already exists\.$/))) return t('validation.fetchExists', { name: match[1] });
  if ((match = message.match(/^At most (\d+) data sources per channel\.$/))) return t('validation.fetchLimit', { max: match[1] });
  return message;
}
