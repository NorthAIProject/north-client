package crews

import (
	"context"

	"github.com/google/uuid"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/NorthAIProject/north-client/internal/crews/crew"
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
	if out.Notify, out.MutedUntil, err = s.repo.MemberSettings(ctx, crewID, viewerID); err != nil {
		return Space{}, err
	}

	members, err := s.repo.Members(ctx, crewID)
	if err != nil {
		return Space{}, err
	}
	out.Crew = Crew{ID: c.ID, Name: c.Name, OwnerID: c.OwnerID, Code: c.Code, Members: len(members)}
	out.Members = make([]SpaceMember, 0, len(members))
	for _, m := range members {
		role := m.Role
		if m.ID == c.OwnerID {
			role = crew.RoleOwner
		}
		out.Members = append(out.Members, SpaceMember{Person: Person{ID: m.ID, Name: m.DisplayName, Handle: util.Val(m.Handle)}, Role: role})
	}

	channels, err := s.Channels(ctx, crewID, viewerID)
	if err != nil {
		return Space{}, err
	}
	unread, err := s.repo.Unread(ctx, viewerID)
	if err != nil {
		return Space{}, err
	}
	out.Channels = make([]SpaceChannel, 0, len(channels))
	for _, ch := range channels {
		sc := SpaceChannel{Channel: ch}
		for _, u := range unread {
			if u.ChannelID == ch.ID {
				sc.Unread, sc.Mentions = u.Count, u.Mentions
			}
		}
		out.Channels = append(out.Channels, sc)
	}
	return out, nil
}
