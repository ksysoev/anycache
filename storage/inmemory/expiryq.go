package inmemory

type expiryQueue []*cacheItem

func (e expiryQueue) Len() int           { return len(e) }
func (e expiryQueue) Less(i, j int) bool { return e[i].expiry.Before(*e[j].expiry) }
func (e expiryQueue) Swap(i, j int) {
	e[i], e[j] = e[j], e[i]
	e[i].expiryPos = i
	e[j].expiryPos = j
}

func (e *expiryQueue) Push(x any) {
	n := len(*e)
	item, _ := x.(*cacheItem)
	*e = append(*e, item)
	item.expiryPos = n
}

func (e *expiryQueue) Pop() any {
	old := *e
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.expiryPos = -1
	*e = old[0 : n-1]

	return item
}
