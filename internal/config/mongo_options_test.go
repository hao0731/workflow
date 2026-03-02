package config

import "testing"

func TestConfigMongoClientOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cfg      *Config
		wantErr  bool
		wantAuth bool
		wantUser string
		wantPass string
	}{
		{
			name:     "uri only",
			cfg:      &Config{MongoURI: "mongodb://localhost:27017"},
			wantErr:  false,
			wantAuth: false,
		},
		{
			name: "username and password",
			cfg: &Config{
				MongoURI:      "mongodb://localhost:27017",
				MongoUsername: "workflow_user",
				MongoPassword: "workflow_pass",
			},
			wantErr:  false,
			wantAuth: true,
			wantUser: "workflow_user",
			wantPass: "workflow_pass",
		},
		{
			name: "username only fails",
			cfg: &Config{
				MongoURI:      "mongodb://localhost:27017",
				MongoUsername: "workflow_user",
			},
			wantErr: true,
		},
		{
			name: "password only fails",
			cfg: &Config{
				MongoURI:      "mongodb://localhost:27017",
				MongoPassword: "workflow_pass",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := tt.cfg.MongoClientOptions()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}

			if opts == nil {
				t.Fatalf("expected options, got nil")
			}

			if (opts.Auth != nil) != tt.wantAuth {
				t.Fatalf("unexpected auth presence: got %v want %v", opts.Auth != nil, tt.wantAuth)
			}

			if tt.wantAuth {
				if opts.Auth.Username != tt.wantUser || opts.Auth.Password != tt.wantPass {
					t.Fatalf("unexpected auth values: got (%s, %s)", opts.Auth.Username, opts.Auth.Password)
				}
			}
		})
	}
}
