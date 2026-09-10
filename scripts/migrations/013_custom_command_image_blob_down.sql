-- Reverting drops every stored image permanently: image_url, the only other
-- place an image was ever referenced, holds expired Discord CDN links that
-- cannot be revived. Down is data loss, not a round trip.
ALTER TABLE custom_commands
    DROP COLUMN image_data,
    DROP COLUMN image_name;
