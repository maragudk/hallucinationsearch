package html

import (
	. "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

type HomePageProps struct {
	PageProps
}

func HomePage(props HomePageProps) Node {
	props.Title = "Hallucination Search"
	props.Description = "Hallucination Search"

	return Page(props.PageProps,
		Div(Class("flex flex-col items-center justify-center gap-8 py-16 md:py-24"),
			H1(Class("text-5xl md:text-7xl font-bold tracking-tight text-center bg-gradient-to-r from-fuchsia-600 via-pink-500 to-purple-600 bg-clip-text text-transparent bg-[length:200%_auto] animate-gradient-shift"),
				Text("Hallucination Search"),
			),
			searchForm("", "w-full max-w-2xl"),
			P(Class("text-xs text-gray-400 max-w-md text-center"),
				Text("Nothing you see here is real."),
			),
		),
	)
}

// searchForm renders the GET form that submits to /?q=... -- used on both the home
// and results pages.
func searchForm(value, wrapperClass string) Node {
	return Form(
		Action("/"), Method("get"), Class(wrapperClass),
		Div(Class("flex gap-2"),
			Input(
				Type("search"), Name("q"), Value(value),
				AutoComplete("off"), AutoFocus(),
				Class("grow min-w-0 px-3 sm:px-4 py-3 rounded-md border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-primary-500 focus:border-primary-500"),
			),
			Button(
				Type("submit"),
				Class("cursor-pointer px-3 sm:px-5 py-3 rounded-md bg-primary-600 hover:bg-primary-500 text-white font-medium"),
				Text("Search"),
			),
		),
	)
}
