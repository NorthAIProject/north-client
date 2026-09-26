// Package fasting tracks fasting windows: start one, see how far in you are,
// end it. A fast is its own log rather than an absence of food logs, because
// "I have not logged dinner" and "I am fasting" are different statements.
package fasting

import "github.com/NorthAIProject/north-client/internal/fasting/fast"

type Session = fast.Session
