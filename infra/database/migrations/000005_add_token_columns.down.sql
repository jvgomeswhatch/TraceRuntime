ALTER TABLE tasks
    DROP COLUMN IF EXISTS prompt_tokens,
    DROP COLUMN IF EXISTS completion_tokens,
    DROP COLUMN IF EXISTS tokens_per_second,
    DROP COLUMN IF EXISTS model;
