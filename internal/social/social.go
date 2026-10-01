// Package social owns the connections between accounts: public handles,
// invite links, follows that need the other person's yes, and blocks.
//
// It decides who is connected to whom and nothing about what they may see of
// each other: a follow here grants no access to anybody's data by itself.
package social

import "github.com/NorthAIProject/north-client/internal/social/friend"

type (
	Person        = friend.Person
	Connection    = friend.Connection
	Invite        = friend.Invite
	InvitePreview = friend.InvitePreview
	Overview      = friend.Overview
	Relationship  = friend.Relationship
	Profile       = friend.Profile
)

const (
	StatusPending  = friend.StatusPending
	StatusAccepted = friend.StatusAccepted

	ChannelLink     = friend.ChannelLink
	ChannelMessages = friend.ChannelMessages
	ChannelX        = friend.ChannelX
	ChannelFacebook = friend.ChannelFacebook
	ChannelContacts = friend.ChannelContacts
)
