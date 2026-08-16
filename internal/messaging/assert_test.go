package messaging

import (
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/users"
)

// The production types must satisfy the narrow interfaces this package
// declares. Asserted in a test file so the check costs nothing at runtime and
// this package's own dependencies stay as small as they read.
var (
	_ Coach   = (*coach.Service)(nil)
	_ Threads = (*conversations.Service)(nil)
	_ Users   = (*users.Service)(nil)
	_ Quotas  = (*quota.Service)(nil)
)
