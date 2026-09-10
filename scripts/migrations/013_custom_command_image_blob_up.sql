-- Store custom-command images as bytes instead of a Discord CDN link.
--
-- /addcom took the attachment's URL and saved it. Attachments passed as
-- slash-command options are served from cdn.discordapp.com/ephemeral-
-- attachments/ with a signed, time-limited URL (the ?ex=&is=&hm= params),
-- and Discord does not retain that content. Every stored link therefore
-- died within hours of being created, and the keyword reply rendered an
-- empty embed box from then on.
--
-- The fix is to download the bytes while the URL is still valid and
-- re-upload them as a real attachment on every use, so the bot never
-- depends on Discord retaining anything.
--
-- image_url is deliberately kept rather than dropped: every existing value
-- is already dead, but dropping the column would make this migration
-- irreversible in practice. The serving path prefers image_data and
-- ignores image_url when data is present.
ALTER TABLE custom_commands
    ADD COLUMN image_data LONGBLOB NULL COMMENT 'raw image bytes, re-uploaded on each use',
    ADD COLUMN image_name VARCHAR(255) NULL COMMENT 'original filename, used for the attachment:// reference and to infer type';
