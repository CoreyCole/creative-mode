package layouts

// RootArgs defines the arguments for the Root layout
type RootArgs struct {
	Title         string
	CurrentPath   string
	Commit        string
	HideFooter    bool
	HideMayorCTA  bool
	FixedViewport bool // Lock body to 100dvh with overflow hidden (for chat-style pages)
}
