// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/** Translator shape intentionally stays smaller than the app i18n interface.
 * Server actions and client editors can both adapt their locale translator. */
export type ValidationTranslator = (
  key: string,
  params?: Record<string, string | number>
) => string;

interface ValidationRule {
  pattern: RegExp;
  key: string;
  parameters: readonly string[];
}

const RULES: readonly ValidationRule[] = [
  { pattern: /^Command name is required\.$/, key: 'validation.commandNameRequired', parameters: [] },
  { pattern: /^Command name must be at most (\d+) characters\.$/, key: 'validation.commandNameMax', parameters: ['max'] },
  { pattern: /^Command name cannot contain spaces\.$/, key: 'validation.commandNameSpaces', parameters: [] },
  { pattern: /^Command name only carries the "!" in chat\. Leave it out here\.$/, key: 'validation.commandNameBang', parameters: [] },
  { pattern: /^Alternate name "([\s\S]*)" is required\.$/, key: 'validation.alternateNameRequired', parameters: ['name'] },
  { pattern: /^Alternate name "([\s\S]*)" must be at most (\d+) characters\.$/, key: 'validation.alternateNameMax', parameters: ['name', 'max'] },
  { pattern: /^Alternate name "([\s\S]*)" cannot contain spaces\.$/, key: 'validation.alternateNameSpaces', parameters: ['name'] },
  { pattern: /^Alternate name "([\s\S]*)" only carries the "!" in chat\. Leave it out here\.$/, key: 'validation.alternateNameBang', parameters: ['name'] },
  { pattern: /^"([\s\S]*)" is already the command's own name\.$/, key: 'validation.aliasOwnName', parameters: ['name'] },
  { pattern: /^"([\s\S]*)" is listed twice\.$/, key: 'validation.aliasDuplicate', parameters: ['name'] },
  { pattern: /^Response is required\.$/, key: 'validation.responseRequired', parameters: [] },
  { pattern: /^Response can be at most (\d+) lines\. Each line is sent as its own chat message\.$/, key: 'validation.responseLinesMax', parameters: ['max'] },
  { pattern: /^Each line must be at most (\d+) characters\.$/, key: 'validation.responseCharsMax', parameters: ['max'] },
  { pattern: /^Response cannot contain control characters\.$/, key: 'validation.responseControlChars', parameters: [] },
  { pattern: /^A response can reference at most (\d+) different fetched values \(\{urlfetch:…\}\)\.$/, key: 'validation.responseFetchMax', parameters: ['max'] },
  { pattern: /^Cooldown must be between 0 and (\d+) seconds\.$/, key: 'validation.cooldownRange', parameters: ['max'] },
  { pattern: /^Cooldown must be a whole number of seconds\.$/, key: 'validation.cooldownInteger', parameters: [] },
  { pattern: /^User restriction must be a numeric Twitch user id\.$/, key: 'validation.userIdNumeric', parameters: [] },
  { pattern: /^Definition name is required\.$/, key: 'validation.definitionNameRequired', parameters: [] },
  { pattern: /^Definition name must be at most (\d+) characters\.$/, key: 'validation.definitionNameMax', parameters: ['max'] },
  { pattern: /^Use lower\-case letters, digits and underscores only\.$/, key: 'validation.definitionNameCharset', parameters: [] },
  { pattern: /^URL is required\.$/, key: 'validation.urlRequired', parameters: [] },
  { pattern: /^URL must be at most (\d+) characters\.$/, key: 'validation.urlMax', parameters: ['max'] },
  { pattern: /^URL must start with https:\/\/$/, key: 'validation.urlHttps', parameters: [] },
  { pattern: /^URL must point at a public https host\.$/, key: 'validation.urlPublic', parameters: [] },
  { pattern: /^Pick plain or json\.$/, key: 'validation.fetchKind', parameters: [] },
  { pattern: /^A plain fetch reads the whole body\. Clear the path or switch to json\.$/, key: 'validation.plainPath', parameters: [] },
  { pattern: /^Path can be at most (\d+) segments deep\.$/, key: 'validation.pathDepth', parameters: ['max'] },
  { pattern: /^"([\s\S]*)" cannot be used as a path segment: letters, digits, "-" and "_" only\.$/, key: 'validation.pathSegment', parameters: ['segment'] },
  { pattern: /^Key label must be at most (\d+) characters\.$/, key: 'validation.keyLabelMax', parameters: ['max'] },
  { pattern: /^A data source named "([\s\S]*)" already exists\.$/, key: 'validation.fetchExists', parameters: ['name'] },
  { pattern: /^At most (\d+) data sources per channel\.$/, key: 'validation.fetchLimit', parameters: ['max'] },
];

/** Translate known validator messages while preserving captured user values.
 * Unknown service diagnostics pass through unchanged. */
export function translateValidationMessage(message: string | undefined, t: ValidationTranslator): string | undefined {
  if (!message) return message;
  for (const rule of RULES) {
    const match = rule.pattern.exec(message);
    if (!match) continue;
    const params = Object.fromEntries(rule.parameters.map((name, index) => [name, match[index + 1]]));
    return t(rule.key, params);
  }
  return message;
}
