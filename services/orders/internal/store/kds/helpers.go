package kds

import "bakehouse/services/orders/ordersapi"

// prepWindow is the order's preparation window: the shortest minimum and the
// longest maximum over the lines still being made.
func prepWindow(items []ordersapi.OrderItem) (minMins, maxMins int) {
	first := true
	for _, i := range items {
		if i.KOTStatus == ordersapi.ItemKOTCancelled || isCancelledLine(i.ProductionStatus) {
			continue
		}
		if first || i.MinPrepMinutes < minMins {
			minMins = i.MinPrepMinutes
		}
		if i.MaxPrepMinutes > maxMins {
			maxMins = i.MaxPrepMinutes
		}
		first = false
	}
	return minMins, maxMins
}

func isCancelledLine(productionStatus string) bool {
	return productionStatus == ordersapi.ItemCancelled || productionStatus == ordersapi.ItemCancel
}
