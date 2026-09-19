# ItsBagelBot

Shared language for ItsBagelBot: Premium access, giveaways, and public channel pages.

## Language

**Premium**:
ItsBagelBot's enhanced access tier. A purchase and a promotional award are both Premium while they cover now. A promotional award is not a Tebex subscription.
_Avoid_: Subscription when referring only to access.

**Giveaway**:
An admin-run promotion that automatically awards a selected number of Premium months by a random draw from eligible registered bot accounts with completed onboarding. VIP, current staff, test, and inactive accounts cannot enter.

**Winner**:
An eligible account selected by a giveaway draw and owed its prize, including while delivery is pending. The account may win again in another giveaway.
_Avoid_: Fulfilled award when referring only to selection.

**Prize month**:
A period of Premium matching one month of the Tebex monthly Premium offer, awarded through a giveaway without charging the winner.
_Avoid_: Paid month, subscription payment.

**Prize duration**:
The whole number of consecutive prize months awarded to each winner of a giveaway, from 1 through 12. Winner count cannot exceed the eligible pool.
_Avoid_: Entry window, draw schedule when referring to the awarded Premium period.

**Pending award**:
A prize still owed to a selected winner whose delivery has not been confirmed. An unresolved renewal postponement leaves the award pending.

**VIP**:
An account with permanent Premium access, excluded from giveaway eligibility.

**Active account**:
A bot account whose bot-enable setting is on.
_Avoid_: Recently active when referring to this setting.

**Test account**:
An account explicitly designated for testing rather than ordinary bot use, excluded from giveaway eligibility.

**Current staff**:
An account with active staff membership, excluded from giveaway eligibility.

**Former staff**:
An account whose staff membership has ended, eligible for giveaways when the other entry requirements are met.

**Tebex subscription**:
An agreement for recurring payments for ItsBagelBot Premium through Tebex.
_Avoid_: Premium when referring to the recurring payment agreement.

**Renewal**:
A scheduled recurring payment under a Tebex subscription.
_Avoid_: Access expiry when referring to a billing event.

**Commands page**:
The public web page listing one channel's chat commands, the page `!commands` links to. Each channel has exactly one.
_Avoid_: Command list (that is the dashboard's management view), channel page.

**Public commands page** / **Hidden commands page**:
A commands page is public (anyone with the link can open it, `!commands` shares the link) or hidden (the link answers as if the channel did not exist, `!commands` says the list is not public). Hiding the page does not disable any command.
_Avoid_: Private, disabled, unpublished.

## Language: Local Time module

**Home zone**:
The timezone a broadcaster picked for the Local Time module. One per channel, may be unset.
_Avoid_: Default timezone, streamer zone.

**Home time**:
The answer to a bare `!time`: the current time in the home zone.
_Avoid_: Local time when referring to the reply rather than the module.

**Place**:
What a viewer typed after `!time`: a city, a timezone name, an abbreviation or a UTC offset. A place is either recognized or unknown; an unknown place is never silently replaced by the home zone.

**Place lookup**:
The answer to `!time <place>`: the current time at a recognized place, using the channel's clock format. Works whether or not the home zone is set.
