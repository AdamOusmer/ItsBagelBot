// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
  { pattern: /^Command name is required\.$/, key: 'commandNameRequired', parameters: [] },
  { pattern: /^Command name must be at most (\d+) characters\.$/, key: 'commandNameMax', parameters: ['max'] },
  { pattern: /^Command name cannot contain spaces\.$/, key: 'commandNameSpaces', parameters: [] },
  { pattern: /^Command name only carries the "!" in chat\. Leave it out here\.$/, key: 'commandNameBang', parameters: [] },
  { pattern: /^Alternate name "([\s\S]*)" is required\.$/, key: 'alternateNameRequired', parameters: ['name'] },
  { pattern: /^Alternate name "([\s\S]*)" must be at most (\d+) characters\.$/, key: 'alternateNameMax', parameters: ['name', 'max'] },
  { pattern: /^Alternate name "([\s\S]*)" cannot contain spaces\.$/, key: 'alternateNameSpaces', parameters: ['name'] },
  { pattern: /^Alternate name "([\s\S]*)" only carries the "!" in chat\. Leave it out here\.$/, key: 'alternateNameBang', parameters: ['name'] },
  { pattern: /^"([\s\S]*)" is already the command's own name\.$/, key: 'aliasOwnName', parameters: ['name'] },
  { pattern: /^"([\s\S]*)" is listed twice\.$/, key: 'aliasDuplicate', parameters: ['name'] },
  { pattern: /^Response is required\.$/, key: 'responseRequired', parameters: [] },
  { pattern: /^Response can be at most (\d+) lines\. Each line is sent as its own chat message\.$/, key: 'responseLinesMax', parameters: ['max'] },
  { pattern: /^Each line must be at most (\d+) characters\.$/, key: 'responseCharsMax', parameters: ['max'] },
  { pattern: /^Response cannot contain control characters\.$/, key: 'responseControlChars', parameters: [] },
  { pattern: /^A response can reference at most (\d+) different fetched values \(\{urlfetch:…\}\)\.$/, key: 'responseFetchMax', parameters: ['max'] },
  { pattern: /^Cooldown must be between 0 and (\d+) seconds\.$/, key: 'cooldownRange', parameters: ['max'] },
  { pattern: /^Cooldown must be a whole number of seconds\.$/, key: 'cooldownInteger', parameters: [] },
  { pattern: /^User restriction must be a numeric Twitch user id\.$/, key: 'userIdNumeric', parameters: [] },
  { pattern: /^Definition name is required\.$/, key: 'definitionNameRequired', parameters: [] },
  { pattern: /^Definition name must be at most (\d+) characters\.$/, key: 'definitionNameMax', parameters: ['max'] },
  { pattern: /^Use lower\-case letters, digits and underscores only\.$/, key: 'definitionNameCharset', parameters: [] },
  { pattern: /^URL is required\.$/, key: 'urlRequired', parameters: [] },
  { pattern: /^URL must be at most (\d+) characters\.$/, key: 'urlMax', parameters: ['max'] },
  { pattern: /^URL must start with https:\/\/$/, key: 'urlHttps', parameters: [] },
  { pattern: /^URL must point at a public https host\.$/, key: 'urlPublic', parameters: [] },
  { pattern: /^Pick plain or json\.$/, key: 'fetchKind', parameters: [] },
  { pattern: /^A plain fetch reads the whole body\. Clear the path or switch to json\.$/, key: 'plainPath', parameters: [] },
  { pattern: /^Path can be at most (\d+) segments deep\.$/, key: 'pathDepth', parameters: ['max'] },
  { pattern: /^"([\s\S]*)" cannot be used as a path segment: letters, digits, "-" and "_" only\.$/, key: 'pathSegment', parameters: ['segment'] },
  { pattern: /^Key label must be at most (\d+) characters\.$/, key: 'keyLabelMax', parameters: ['max'] },
  { pattern: /^A data source named "([\s\S]*)" already exists\.$/, key: 'fetchExists', parameters: ['name'] },
  { pattern: /^At most (\d+) data sources per channel\.$/, key: 'fetchLimit', parameters: ['max'] },
];

export function translateValidationMessage(message: string | undefined, t: ValidationTranslator): string | undefined {
  if (!message) return message;
  for (const rule of RULES) {
    const match = rule.pattern.exec(message);
    if (!match) continue;
    const params = Object.fromEntries(rule.parameters.map((name, index) => [name, match[index + 1]]));
    return t(`validation.${rule.key}`, params);
  }
  return message;
}
