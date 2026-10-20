package crews

import (
	"context"

	"github.com/google/uuid"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/NorthAIProject/north-client/internal/crews/crew"
	crewsdb "github.com/NorthAIProject/north-client/internal/crews/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Space is the crew as its chat shows it to viewerID, a member.
func (s *Service) Space(ctx context.Context, crewID, viewerID uuid.UUID) (Space, error) {
	c, err := s.get(ctx, crewID, viewerID)
	if err != nil {
		return Space{}, err
	}
	out := Space{}
	if out.MyRole, err = s.role(ctx, c, viewerID); err != nil {
		return Space{}, err
	}
	settings, err := s.q.MemberSettings(ctx, crewsdb.MemberSettingsParams{CrewID: crewID, UserID: viewerID})
	if err != nil {
		return Space{}, apperr.Wrap(err, "member settings")
	}
	out.Notify, out.MutedUntil = settings.Notify, settings.MutedUntil

	members, err := s.q.Members(ctx, crewID)
	if err != nil {
		return Space{}, apperr.Wrap(err, "list members")
	}
	out.Crew = Crew{ID: c.ID, Name: c.Name, OwnerID: c.OwnerID, Code: c.Code, Members: len(members)}
	out.Members = make([]SpaceMember, 0, len(members))
	for _, m := range members {
		role := Role(m.Role)
		if m.ID == c.OwnerID {
			role = crew.RoleOwner
		}
		out.Members = append(out.Members, SpaceMember{Person: Person{ID: m.ID, Name: m.DisplayName, Handle: util.Val(m.Handle)}, Role: role})
	}

	channels, err := s.Channels(ctx, crewID, viewerID)
	if err != nil {
		return Space{}, err
	}
	unread, err := s.q.Unread(ctx, viewerID)
	if err != nil {
		return Space{}, apperr.Wrap(err, "unread")
	}
	out.Channels = make([]SpaceChannel, 0, len(channels))
	for _, ch := range channels {
		sc := SpaceChannel{Channel: ch}
		for _, u := range unread {
			if u.ChannelID == ch.ID {
				sc.Unread, sc.Mentions = int(u.Unread), int(u.Mentions)
			}
		}
		out.Channels = append(out.Channels, sc)
	}
	return out, nil
}
