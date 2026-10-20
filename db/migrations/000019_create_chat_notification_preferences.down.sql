DROP TABLE IF EXISTS conversation_notification_preferences;

ALTER TABLE messages
    DROP COLUMN IF EXISTS mentioned_user_ids;
