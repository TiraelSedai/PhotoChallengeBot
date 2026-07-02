# PhotoChallengeBot

Telegram bot that runs recurring photo challenges in one main chat: it announces a theme, accepts competition photos, closes acceptance, runs voting in private chats, and publishes results.

## Language

### Roles & places

**Main chat**:
The shared participant chat where the bot posts announcements, reminders, the vote-start, and results.
_Avoid_: group, channel.

**Admin chat**:
The separate chat from which admins drive the bot.
_Avoid_: control room, ops chat.

**Admin**:
Anyone who writes to the bot in the admin chat.
_Avoid_: moderator, operator.

**Participant**:
A user who has entered a competition photo in a challenge.
_Avoid_: user (too generic), player.

### Challenge lifecycle

**Challenge**:
One themed photo contest in a main chat, with a theme, a hashtag, and an acceptance window. At most one is active per main chat at a time.
_Avoid_: contest, competition, round.

**Competition photo**:
The single current photo a participant has entered in a challenge, matched by the challenge hashtag; a new submission replaces the previous one.
_Avoid_: entry, submission, post.

**Acceptance window**:
The period during which competition photos are accepted, closing at 18:00 MSK on the last day.
_Avoid_: intake period, submission phase.

**Vote-start**:
The pinned main-chat message that opens voting and links to the private voting UI.
_Avoid_: voting announcement, ballot open.

**Results**:
The pinned main-chat post declaring the winner(s) followed by ranked albums of all works.
_Avoid_: outcome, summary, standings.

**Achievement**:
The extra main-chat message emitted when a participant reaches their 1st, 3rd, 5th, or 7th win.
_Avoid_: badge, milestone message.

### Voting

**Vote token**:
The opaque, intentionally forgeable identifier of a challenge's voting, of the form `{main_chat_id}_{challenge_id}`, carried in the deep-link.
_Avoid_: ballot id, session key.

**Self-like**:
The one like automatically counted for a participant on their own competition photo; a manual like on one's own photo is never counted.
_Avoid_: auto-vote, self-vote.

**Topic suggestion**:
A theme proposed with the `#тема` tag during a challenge's voting, for a future challenge.
_Avoid_: theme idea.

**Topic-report**:
The admin-chat message listing the topic suggestions collected during a challenge's voting.
_Avoid_: theme digest.

### Publication

**Phase**:
One at-most-once lifecycle side-effect the bot publishes for a challenge — reminder, vote-start, results, achievement, or topic-report. Each phase is published exactly once across restarts and concurrent scheduler ticks.
_Avoid_: step, stage, job, task.

**Lease**:
Exclusive, time-boxed ownership of a phase that a worker claims before publishing it and holds until it commits or is released; released for retry only on recoverable failures that occur before the message is sent.
_Avoid_: lock, mutex.
