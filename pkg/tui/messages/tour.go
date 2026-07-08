package messages

// Tour messages drive the interactive getting-started tour.
type (
	// StartTourMsg starts (or restarts) the interactive getting-started
	// tour. Emitted by the /getting-started slash command, the command
	// palette entry, and the first-run offer dialog.
	StartTourMsg struct{}

	// TourFinishedMsg is emitted by the tour component when the tour ends.
	// Completed is true when the user reached the final step, false when
	// the tour was quit early.
	TourFinishedMsg struct{ Completed bool }
)
