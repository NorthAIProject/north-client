package social

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestSocialShapes(t *testing.T) {
	t.Parallel()

	api := NewAPI(nil, "https://kheprios.com/")
	ana := Person{ID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), DisplayName: "Ana", Handle: "ana_runs"}
	joao := Person{ID: uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"), DisplayName: "João", Handle: ""}
	since := time.Date(2026, 9, 30, 20, 0, 0, 0, time.UTC)
	invite := Invite{Code: "k7m2p9qx4r", Channel: ChannelLink}

	apitest.AssertGolden(t, "social.golden.json", api.projectOverview(Overview{
		Handle: "me_too", Invite: invite, Joined: 2,
		Requests:  []Connection{{Person: joao, Status: StatusPending, Since: since}},
		Followers: []Connection{{Person: ana, Status: StatusAccepted, Since: since}},
		Following: []Connection{{Person: ana, Status: StatusAccepted, Since: since}},
	}))
	apitest.AssertGolden(t, "invite.golden.json", api.projectInvite(invite))
	apitest.AssertGolden(t, "invite-preview.golden.json", InvitePreviewView{Code: invite.Code, Inviter: projectPerson(ana)})
	apitest.AssertGolden(t, "public-profile.golden.json", ProfileView{
		PersonView: projectPerson(ana), Followers: 12, Following: 9,
		Relationship: RelationshipView{Following: StatusPending, FollowsYou: true},
	})
	apitest.AssertGolden(t, "connection.golden.json", projectConnection(Connection{Person: ana, Status: StatusPending, Since: since}))
	apitest.AssertGolden(t, "redeem.golden.json", RedeemView{Redeemed: true})
	apitest.AssertGolden(t, "handle.golden.json", HandleView{Handle: "ana_runs"})
	apitest.AssertGolden(t, "contacts-match.golden.json", projectMatches([]Connection{
		{Person: ana, Status: ""}, {Person: Person{ID: joao.ID, DisplayName: "João", Handle: "joao"}, Status: StatusPending},
	}))
}
