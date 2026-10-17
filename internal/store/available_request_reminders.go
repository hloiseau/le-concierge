package store

// The existing outbox and daily delivery record also carry optional request
// nudges. Their added-at fact is separate from every removal deadline.
const availableRequestRemindersSchema = `
ALTER TABLE reminder_preferences ADD COLUMN available_unwatched INTEGER NOT NULL DEFAULT 0 CHECK(available_unwatched IN (0,1));
ALTER TABLE reminder_outbox ADD COLUMN available_at INTEGER;`
