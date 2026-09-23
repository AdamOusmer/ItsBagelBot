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

**Trial channel**:
A Twitch channel an administrator observes before its broadcaster has registered with ItsBagelBot. Its chat can exercise the bot's processing path, but the bot never acts in that channel on the trial's behalf. A registered account cannot remain a trial channel.
_Avoid_: Test account, registered channel when referring to this observation.

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

## Language: reply templates

**Variable**:
A named value the bot can fill into a reply, such as the viewer's follow time. One variable has one name, one guide entry and one set of copy, however many spellings it accepts.
_Avoid_: Token, family when referring to the concept a broadcaster picks.

**Token**:
One complete spelling of a variable inside a reply, braces included, such as `{followage:alex}`. A variable accepts one or more tokens.
_Avoid_: Variable when referring to the literal text typed into a reply.

**Form**:
One shape a variable's token can take, with the rule for what goes after the colon. `{followage}` and `{followage:<login>}` are two forms of one variable.
_Avoid_: Syntax.

**Surface**:
A place where a broadcaster writes a reply template: a custom command, a follow alert, a reward reply. A surface offers a fixed set of variables, and the same variable name can mean something different on two surfaces.

**Chip**:
A dashboard button that inserts one token into a reply field. Presentation only.

**Scope**:
An internal resolver group in the bot engine. Never shown to broadcasters.

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

## Language: Timers

**Timer**:
A broadcaster-authored repeating chat message, stream-only. Keeps its message and interval; may also carry a gate and one or more stops.
_Avoid_: Scheduled message, reminder.

**Tick**:
One check of a timer, driven by its Valkey schedule key's expiry. A tick either fires, skips, or stops the timer.

**Gate**:
A per-tick condition checked at every tick. A tick whose gate fails skips that one post and re-arms at the normal interval; the timer keeps its cadence and fires on the next tick that passes. v1 ships one gate: the chat activity gate.
_Avoid_: Filter, condition when referring to an end condition (that is a stop).

**Stop**:
A condition that ends a timer: once reached, the timer does not post and does not re-arm until the next stream. v1 ships two stops: the fire cap and the end date.
_Avoid_: Gate when referring to an end condition.

**Chat activity gate**:
The v1 gate: a timer fires only once at least a set number of chat lines have arrived since it last fired (or, for its first tick, since it armed). Configured per timer as a line count from 0 (off) to 100.
_Avoid_: Activity filter.

**Fire cap**:
The v1 stop limiting how many times a timer may post in one stream: 0 (unlimited) to 100. Resets on the next stream.
_Avoid_: Post limit, rate limit (this is a per-stream count, not a rate).

**End date**:
The v1 stop ending a timer at a fixed instant. A timer past its end date shows as Ended and stays editable; nothing about it is auto-disabled.
_Avoid_: Expiry, deadline.

**Skipped tick**:
A tick whose gate failed: no post, cadence unchanged, no effect on any stop. A skipped tick is not a fire.
_Avoid_: Fire, miss.
