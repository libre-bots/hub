package privacy

import (
	"context"
	"fmt"
)

type CategoryCount struct {
	Category string
	Count    int64
}

// DataOwner reports, exports and deletes the data a bot keeps about a user or a guild.
type DataOwner interface {
	CountUser(ctx context.Context, userID uint64) ([]CategoryCount, error)
	// ExportUser returns a JSON-encodable value; ids inside it must be strings, as the envelope does for user_id.
	ExportUser(ctx context.Context, userID uint64) (any, error)
	DeleteUser(ctx context.Context, userID uint64) ([]CategoryCount, error)
	CountGuild(ctx context.Context, guildID uint64) ([]CategoryCount, error)
	// DeleteGuild must not write the guilds table; the caller may hold a lock on the guild's row.
	DeleteGuild(ctx context.Context, guildID uint64) ([]CategoryCount, error)
}

// Owners maps bots to their DataOwner. Fill it before the server starts.
type Owners struct {
	byBot map[string]DataOwner
}

func NewOwners() *Owners { return &Owners{byBot: map[string]DataOwner{}} }

// Set panics on a nil owner or a bot set twice.
func (o *Owners) Set(bot string, owner DataOwner) {
	if owner == nil {
		panic(fmt.Sprintf("privacy: nil owner for bot %q", bot))
	}
	if _, ok := o.byBot[bot]; ok {
		panic(fmt.Sprintf("privacy: owner for bot %q set twice", bot))
	}
	o.byBot[bot] = owner
}

// For never returns nil: a bot without an owner holds no data.
func (o *Owners) For(bot string) DataOwner {
	if owner, ok := o.byBot[bot]; ok {
		return owner
	}
	return none{}
}

type none struct{}

func (none) CountUser(context.Context, uint64) ([]CategoryCount, error)  { return nil, nil }
func (none) ExportUser(context.Context, uint64) (any, error)             { return nil, nil }
func (none) DeleteUser(context.Context, uint64) ([]CategoryCount, error) { return nil, nil }
func (none) CountGuild(context.Context, uint64) ([]CategoryCount, error) { return nil, nil }
func (none) DeleteGuild(context.Context, uint64) ([]CategoryCount, error) {
	return nil, nil
}
