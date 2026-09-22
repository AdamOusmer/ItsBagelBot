import { translateValidationMessage } from '../../../../kit/lib/engine/validation-messages';
import { translate, type Locale, type MessageKey } from '@bagel/kit';

const KEYS: Record<string, MessageKey> = {
  'This account already has Premium coverage. Subscribing again is blocked while the current prize or plan is being reconciled.': 'serverErrors.premiumAlreadyHeld',
  'Too many test runs. Each one calls the real API. Wait about 10 seconds and try again.': 'serverErrors.fetchRate',
  'Fix the highlighted fields first.': 'serverErrors.fixFields',
  'The fetch service did not answer. Try again in a moment.': 'serverErrors.fetchService',
  "Invalid input.": 'serverErrors.invalidInput',
  "Invalid counter.": 'serverErrors.invalidCounter',
  "Invalid timer.": 'serverErrors.invalidTimer',
  "Invalid settings.": 'serverErrors.invalidSettings',
  "Could not toggle Discord.": 'serverErrors.discordToggle',
  "missing timer id": 'serverErrors.timerId',
  "missing reward id": 'serverErrors.rewardId',
  "failed": 'serverErrors.failed',

  "Could not create link.": 'serverErrors.createLink',
  "Could not update link.": 'serverErrors.updateLink',
  "Could not revoke link.": 'serverErrors.revokeLink',
  "Could not leave dashboard.": 'serverErrors.leaveDashboard',
  "Label is required.": 'serverErrors.keyLabelRequired',
  "Label must be at most 32 characters.": 'serverErrors.keyLabelLength',
  "Key value is required.": 'serverErrors.keyValueRequired',
  "Nothing could be imported from that source.": 'serverErrors.importEmpty',
  'Not signed in.': 'serverErrors.notSignedIn',
  'Not allowed.': 'serverErrors.notAllowed',
  'Unknown module.': 'serverErrors.unknownModule',
  'Unknown module': 'serverErrors.unknownModule',
  'Premium only while in beta.': 'serverErrors.premiumOnly',
  'Invalid patch.': 'serverErrors.invalidPatch',
  'Could not save the quote.': 'serverErrors.quoteSave',
  'Could not update the quote.': 'serverErrors.quoteUpdate',
  'Could not delete the quote.': 'serverErrors.quoteDelete',
  'Invalid quote number.': 'serverErrors.quoteNumber',
  'Choose a valid quote date.': 'serverErrors.quoteDate',
  'Could not update. Try again in a moment.': 'serverErrors.updateRetry',
  'Could not delete account.': 'serverErrors.deleteAccount',
  'Only the account owner can do that.': 'serverErrors.ownerOnly',
  'Pick at least one section.': 'serverErrors.pickSection',
  'Missing grant.': 'serverErrors.missingGrant',
  'Missing token.': 'serverErrors.missingToken',
  'Missing dashboard.': 'serverErrors.missingDashboard',
  'This command has no editable reply.': 'serverErrors.noEditableReply',
  'Unknown built-in command.': 'serverErrors.unknownBuiltin',
  'Subscriptions are not available right now.': 'serverErrors.subscriptionsUnavailable',
  'Subscription management is not available right now.': 'serverErrors.subscriptionManagementUnavailable',
  'Discord is in beta and open to Premium channels only.': 'serverErrors.discordPremiumOnly',
  'You do not have access to manage billing.': 'serverErrors.billingOwnerOnly',
  'Enter the Twitch username to gift to.': 'serverErrors.giftUsername',
  'That does not look like a Twitch username.': 'serverErrors.giftUsernameShape',
  'Could not verify the current plan. Try again in a moment.': 'serverErrors.planVerify',
  'There is no Tebex subscription to cancel for this account.': 'serverErrors.noSubscription',
  'Gifting is not available right now. Try again in a moment.': 'serverErrors.giftUnavailable',
  'Too many import attempts. Wait a minute and try again.': 'serverErrors.importRate',
  'The parsed import is too large to verify.': 'serverErrors.importTooLarge',
  'The parsed import could not be decoded. Run the preview again.': 'serverErrors.importDecode',
  'That file is too large. Exported bot configs should be well under 20 MB.': 'serverErrors.fileTooLarge',
  'Could not read that file.': 'serverErrors.fileRead',
  'Pick a source to import from.': 'serverErrors.importSource',
  'The importer service did not answer. Try again in a moment.': 'serverErrors.importService',
  'Nothing selected to import.': 'serverErrors.importNothing',
  'The selection could not be decoded. Run the preview again.': 'serverErrors.importSelection',
  'Could not seal the key.': 'serverErrors.sealKey',
  'Could not delete the key.': 'serverErrors.deleteKey',
  'Could not update.': 'serverErrors.updateFailed',
  'id required': 'serverErrors.idRequired',
  'Enter a quote to save.': 'serverErrors.quoteText',
  "Gift notes can't contain links or web addresses. Please remove it and try again.": 'serverErrors.giftLinks',
  'Not found': 'serverErrors.notFound',
  'Invalid request.': 'serverErrors.invalidRequest',
  'Invalid reward.': 'serverErrors.invalidReward',
  'Both the client ID and the client secret are required.': 'serverErrors.appCredentials',
  'Enter your Govee API key.': 'serverErrors.goveeKey',
  'Title is required (max 45 characters).': 'serverErrors.rewardTitle',
  'Response is required.': 'serverErrors.responseRequired',
  'Response cannot contain control characters.': 'serverErrors.responseControl',
  'Cooldown must be a whole number of seconds.': 'serverErrors.cooldownWhole',
  'User restriction must be a numeric Twitch user id.': 'serverErrors.userIdNumeric',
  'Pick a light first.': 'serverErrors.pickLight',
  'Enter a valid point cost.': 'serverErrors.pointCost',
  'Pick a valid colour.': 'serverErrors.colour'
};

export function actionError(locale: Locale, fallback: string): string {
  const validation = translateValidationMessage(fallback, (key, params) => translate(locale, key, params));
  if (validation && validation !== fallback) return validation;
  const replyMax = /^Reply is too long \(max (\d+)(?: characters)?\)\.$/.exec(fallback);
  if (replyMax) {
    const key = 'serverErrors.replyTooLong' as MessageKey;
    const value = translate(locale, key, { max: replyMax[1] });
    return value === key ? fallback : value;
  }
  const key = KEYS[fallback];
  if (!key) return fallback;
  const value = translate(locale, key);
  return value === key ? fallback : value;
}

/** Only translate presentation fields; preserve samples, service data and status. */
export function actionErrorBody(locale: Locale, body: Record<string, unknown>): Record<string, unknown> {
  const result = { ...body };
  if (typeof body.error === 'string') result.error = actionError(locale, body.error);
  if (isFieldErrors(body.errors)) {
    result.errors = Object.fromEntries(Object.entries(body.errors).map(([field, message]) =>
      [field, typeof message === 'string' ? actionError(locale, message) : message]));
  }
  return result;
}

function isFieldErrors(value: unknown): value is Record<string, unknown> {
  if (!value) return false;
  if (Array.isArray(value)) return false;
  return typeof value === 'object';
}
