package controllers

import (
	"slices"

	"github.com/samber/lo"

	"github.com/kuadrant/policy-machinery/machinery"
)

func uniqueSortedPaths(allPaths []lo.Entry[string, []machinery.Targetable]) []lo.Entry[string, []machinery.Targetable] {
	paths := lo.UniqBy(allPaths, func(e lo.Entry[string, []machinery.Targetable]) string { return e.Key })
	slices.SortFunc(paths, func(a, b lo.Entry[string, []machinery.Targetable]) int {
		switch {
		case a.Key < b.Key:
			return -1
		case a.Key > b.Key:
			return 1
		default:
			return 0
		}
	})
	return paths
}
