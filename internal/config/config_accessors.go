package config

// GetBotToken returns the Discord bot token
func (c *Config) GetBotToken() string {
	return c.v.GetString("bot_token")
}

// GetServerID returns the server ID
func (c *Config) GetServerID() string {
	return c.v.GetString("server_id")
}

// GetDatabasePath returns the path to the SQLite database
func (c *Config) GetDatabasePath() string {
	return c.v.GetString("database_path")
}

// GetCryptoSalt returns the crypto salt for random names
func (c *Config) GetCryptoSalt() string {
	return c.v.GetString("crypto_salt")
}

// GetSuperAdmins returns the list of super admin user IDs
func (c *Config) GetSuperAdmins() []string {
	return c.v.GetStringSlice("super_admins")
}

// IsSuperAdmin checks if a user ID is a super admin
func (c *Config) IsSuperAdmin(userID string) bool {
	for _, adminID := range c.GetSuperAdmins() {
		if adminID == userID {
			return true
		}
	}
	return false
}
