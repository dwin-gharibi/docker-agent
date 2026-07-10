// Package ui contains the lean TUI presentation layer: terminal rendering,
// input widgets, reusable views, and plain view-models.
//
// This package may use the shared TUI rendering vocabulary, but it must not
// directly import docker-agent runtime or driver packages. Runtime events and
// app commands are translated by the parent leantui controller package.
package ui
