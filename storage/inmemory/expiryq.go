package inmemory

type expiryQueue []*cacheItem

// Len, Less, and Swap implement the heap.Interface for expiryQueue.
func (e expiryQueue) Len() int { return len(e) }

// Less returns true if the expiry time of the item at index i is before the expiry time of the item at index j.
func (e expiryQueue) Less(i, j int) bool { return e[i].expiry.Before(*e[j].expiry) }

// Swap swaps the items at indices i and j and updates their expiryPos fields accordingly.
func (e expiryQueue) Swap(i, j int) {
	e[i], e[j] = e[j], e[i]
	e[i].expiryPos = i
	e[j].expiryPos = j
}

// Push adds a new item to the expiryQueue and updates its expiryPos field to reflect its position in the queue.
func (e *expiryQueue) Push(x any) {
	n := len(*e)
	item, _ := x.(*cacheItem)
	*e = append(*e, item)
	item.expiryPos = n
}

// Pop removes and returns the item with the earliest expiry time from the expiryQueue, updates its expiryPos field to -1, and returns it.
func (e *expiryQueue) Pop() any {
	old := *e
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.expiryPos = -1
	*e = old[0 : n-1]

	return item
}
