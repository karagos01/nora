package ui

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/store"
)

type SearchView struct {
	app *App

	editor     widget.Editor
	results    []store.Contact
	resultBtns []widget.Clickable
	lastQuery  string
}

func NewSearchView(a *App) *SearchView {
	sv := &SearchView{app: a}
	sv.editor.SingleLine = true
	return sv
}

func (sv *SearchView) Layout(gtx layout.Context) layout.Dimensions {
	// Update search query
	for {
		ev, ok := sv.editor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			sv.doSearch()
		}
	}

	query := sv.editor.Text()
	if query != sv.lastQuery {
		sv.lastQuery = query
		sv.doSearch()
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Search header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(16), Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.H6(sv.app.Theme.Material, "Contacts")
				lbl.Color = ColorText
				return lbl.Layout(gtx)
			})
		}),
		// Search input
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						sz := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
						rr := gtx.Dp(8)
						paint.FillShape(gtx.Ops, ColorInput, clip.RRect{Rect: sz, NE: rr, NW: rr, SE: rr, SW: rr}.Op(gtx.Ops))
						return layout.Dimensions{Size: sz.Max}
					},
					func(gtx layout.Context) layout.Dimensions {
						return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							e := material.Editor(sv.app.Theme.Material, &sv.editor, "Search contacts...")
							e.Color = ColorText
							e.HintColor = ColorTextDim
							return e.Layout(gtx)
						})
					},
				)
			})
		}),
		// Results
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if len(sv.results) == 0 && sv.lastQuery != "" {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(sv.app.Theme.Material, "No contacts found")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				})
			}
			if len(sv.results) == 0 {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(sv.app.Theme.Material, "Type to search your contacts")
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				})
			}

			// Ensure enough buttons
			for len(sv.resultBtns) < len(sv.results) {
				sv.resultBtns = append(sv.resultBtns, widget.Clickable{})
			}

			// Handle clicks
			for i, ct := range sv.results {
				if sv.resultBtns[i].Clicked(gtx) {
					// Open UserPopup for this contact
					pubKey := ct.PublicKey
					name := ct.CustomName
					if name == "" {
						name = ct.AutoName
					}
					// Find user ID on the active server
					userID := ""
					if conn := sv.app.Conn(); conn != nil {
						sv.app.mu.RLock()
						for _, u := range conn.Users {
							if u.PublicKey == pubKey {
								userID = u.ID
								break
							}
						}
						sv.app.mu.RUnlock()
					}
					if userID != "" {
						sv.app.UserPopup.Show(userID, name)
					}
				}
			}

			var items []layout.FlexChild
			for i, ct := range sv.results {
				i, ct := i, ct
				items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return sv.layoutContactItem(gtx, i, ct)
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
		}),
	)
}

func (sv *SearchView) doSearch() {
	if sv.app.Contacts == nil {
		sv.results = nil
		return
	}
	query := strings.TrimSpace(sv.lastQuery)
	if query == "" {
		sv.results = sv.app.Contacts.AllContacts()
		return
	}

	// Parse name#discriminant
	if idx := strings.LastIndex(query, "#"); idx > 0 {
		name := query[:idx]
		disc := query[idx+1:]
		all := sv.app.Contacts.Search(name)
		var filtered []store.Contact
		for _, c := range all {
			if strings.HasPrefix(c.Discriminant, strings.ToLower(disc)) {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) > 0 {
			sv.results = filtered
			return
		}
	}

	sv.results = sv.app.Contacts.Search(query)
}

func (sv *SearchView) layoutContactItem(gtx layout.Context, idx int, ct store.Contact) layout.Dimensions {
	return sv.resultBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := color.NRGBA{}
		if sv.resultBtns[idx].Hovered() {
			bg = ColorHover
		}
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				if bg.A > 0 {
					paint.FillShape(gtx.Ops, bg, clip.Rect{Max: bounds.Max}.Op())
				}
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							name := ct.CustomName
							if name == "" {
								name = ct.AutoName
							}
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Body1(sv.app.Theme.Material, name)
									lbl.Color = UserColor(name)
									lbl.MaxLines = 1
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(sv.app.Theme.Material, "#"+ct.Discriminant)
									lbl.Color = ColorTextDim
									return lbl.Layout(gtx)
								}),
							)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if ct.CustomName != "" && ct.AutoName != "" {
								lbl := material.Caption(sv.app.Theme.Material, "aka "+ct.AutoName)
								lbl.Color = ColorTextDim
								return lbl.Layout(gtx)
							}
							return layout.Dimensions{}
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if ct.Notes != "" {
								lbl := material.Caption(sv.app.Theme.Material, ct.Notes)
								lbl.Color = ColorTextDim
								lbl.MaxLines = 1
								return lbl.Layout(gtx)
							}
							return layout.Dimensions{}
						}),
					)
				})
			},
		)
	})
}
