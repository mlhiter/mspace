CREATE TABLE IF NOT EXISTS user_password_credentials (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  login TEXT NOT NULL CHECK (login = lower(btrim(login)) AND login ~ '^[a-z0-9._-]{1,80}$'),
  password_hash TEXT NOT NULL CHECK (length(password_hash) > 0),
  disabled BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(user_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_password_credentials_login_unique
  ON user_password_credentials (lower(login));
