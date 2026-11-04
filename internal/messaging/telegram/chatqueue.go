package telegram

import "sync"

// chatQueue runs the work for each chat one item at a time, in arrival order.
//
// A coach turn reads the thread, generates and saves, so two at once in one
// chat would each answer without seeing the other. Arrival order is also what
// a person means: a caption-less photo followed by "what do you think?" is one
// thought in two updates.
//
// There is one goroutine per busy chat and none for an idle one, so a quiet bot
// holds nothing. Nothing is dropped when a chat falls behind. Refusing a message
// is what this replaced, and the turns themselves are metered further in.
type chatQueue struct {
	mu sync.Mutex
	// pending holds what is still to run per chat. A key being present means a
	// drain goroutine owns that chat, even when its slice is empty.
	pending map[string][]func()
}

// add queues one item for a chat. If the chat was idle it hands start a function
// that runs the chat's items, for start to run on a new goroutine.
func (q *chatQueue) add(chat string, start func(drain func()), work func()) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.pending == nil {
		q.pending = make(map[string][]func())
	}
	queued, busy := q.pending[chat]
	q.pending[chat] = append(queued, work)
	if !busy {
		start(func() { q.drain(chat) })
	}
}

// drain runs a chat's items until none are left, then forgets the chat. Work
// must not panic: a panic here would take the process down, so callers recover
// inside the function they queue.
func (q *chatQueue) drain(chat string) {
	for {
		work, ok := q.next(chat)
		if !ok {
			return
		}
		work()
	}
}

func (q *chatQueue) next(chat string) (func(), bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	queued := q.pending[chat]
	if len(queued) == 0 {
		delete(q.pending, chat)
		return nil, false
	}
	q.pending[chat] = queued[1:]
	return queued[0], true
}

// busyChats reports how many chats have a drain running.
func (q *chatQueue) busyChats() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}
