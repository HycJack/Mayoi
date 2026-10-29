package main

import (
	"mayoi/internal/settings"
	"mayoi/locales"

	"github.com/egoist/mygo/ui"
)

// aiPermissionName is what a permission is called where it is chosen, as a
// preset of what the assistant may do without asking.
func aiPermissionName(perm string) string {
	switch perm {
	case settings.PermReadOnly:
		return locales.AiPermReadOnly()
	case settings.PermWorkspaceWrite:
		return locales.AiPermWorkspaceWrite()
	case settings.PermFullAccess:
		return locales.AiPermFullAccess()
	}
	return locales.AiPermAsk()
}

// aiPermissionWhy is what the permission costs, in a line.
func aiPermissionWhy(perm string) string {
	switch perm {
	case settings.PermReadOnly:
		return locales.AiPermReadOnlyWhy()
	case settings.PermWorkspaceWrite:
		return locales.AiPermWorkspaceWriteWhy()
	case settings.PermFullAccess:
		return locales.AiPermFullAccessWhy()
	}
	return locales.AiPermAskWhy()
}

// aiPermissionRow is one permission of a list, the one in use marked, as a
// voice or a chat is.
func (a *app) aiPermissionRow(c *ui.Context, chosen, perm string, pick func()) {
	p := a.pal
	on := chosen == perm
	row := ui.ButtonBase(c.Key("ai-perm:"+perm)).Padding(4, 8).Gap(8).Radius(6).
		Justify(ui.Start).AlignItems(ui.Center).Label(aiPermissionName(perm))
	if row.Hovered() {
		row.Background(p.hover)
	}
	if row.Clicked() && !on {
		pick()
	}
	row.Children(func() {
		a.pickMark(c, on, "ai-perms")
		ui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, aiPermissionName(perm)).FontSize(12.5).TextColor(p.text).SingleLine()
			ui.Text(c, aiPermissionWhy(perm)).FontSize(11).TextColor(p.faint)
		})
	})
}

// setPermission keeps perm as what the assistant may do, and says so in the
// panel: the next question is asked under it, and the settings keep it for
// the windows that follow.
func (a *app) setPermission(perm string) {
	if !settings.KnownPermission(perm) || a.settings.AI.Permission() == perm {
		return
	}
	a.settings.AI.Permissions = perm
	a.settings.Changed()
	a.invalidate()
}

// aiPermissionButton is the permission in use, at the right of the field,
// where the question is written: it opens the list that changes it.
func (a *app) aiPermissionButton(c *ui.Context) {
	p := a.pal
	perm := a.settings.AI.Permission()
	b := ui.ButtonBase(c.Key("ai-perm-button")).Padding(2, 6).Gap(4).Radius(4).Height(22).
		Justify(ui.Start).AlignItems(ui.Center).Label(locales.AiPermTitle())
	if b.Hovered() {
		b.Background(p.hover)
	}
	if b.Clicked() {
		a.post(a.togglePermissions)
	}
	b.Children(func() {
		ui.Text(c, aiPermissionName(perm)).FontSize(10.5).TextColor(p.faint).SingleLine()
		ui.Icon(c, iconChevronDown).FontSize(11).TextColor(p.faint)
	})
}

// togglePermissions opens or closes the list of what the assistant may do,
// closing the other lists the panel shows over it.
func (a *app) togglePermissions() {
	p := &a.ai
	p.perms = !p.perms
	p.menu, p.chats, p.models = false, false, false
	a.invalidate()
}

// aiPermissionMenu is the list of permissions over the panel: the one in
// use marked, and the one that asks for nothing asking first.
func (a *app) aiPermissionMenu(c *ui.Context) {
	p := &a.ai
	if !p.perms {
		return
	}
	pal := a.pal
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "ai-perms")
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(360).MaxWidthPercent(92).Margin(titleBarH+8, 0, 0, 0).Radius(10), true).Clip().
				Role(ui.RoleDialog).Label(locales.AiPermTitle())
			box.Children(func() {
				ui.Row(c).Padding(10, 14).Gap(10).AlignItems(ui.Center).
					BorderWidth(0, 0, 1, 0).BorderColor(pal.border).Children(func() {
					ui.Icon(c, iconLock).FontSize(15).TextColor(pal.muted)
					ui.Text(c, locales.AiPermTitle()).FontSize(14).FontWeight(600).TextColor(pal.text).Grow(1).SingleLine()
				})
				ui.Column(c).Key("ai-perm-list").Padding(6).Gap(1).Children(func() {
					for _, perm := range settings.Permissions() {
						name := perm
						a.aiPermissionRow(c, a.settings.AI.Permission(), name, func() {
							a.post(func() { a.choosePermission(name) })
						})
					}
				})
			})
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.post(func() {
					p.perms = false
					a.invalidate()
				})
			}
		})
	})
}

// choosePermission takes a permission of the list: full access asks first,
// as it is the one that stops the cards for good.
func (a *app) choosePermission(perm string) {
	p := &a.ai
	p.perms = false
	if perm == settings.PermFullAccess && a.settings.AI.Permission() != perm {
		p.permAsk = perm
		a.invalidate()
		return
	}
	a.setPermission(perm)
}

// aiPermissionConfirm asks whether the assistant should have full access,
// as the one permission that changes files and runs commands without a
// word.
func (a *app) aiPermissionConfirm(c *ui.Context) {
	p := &a.ai
	if p.permAsk == "" {
		return
	}
	pal := a.pal
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "ai-perm-confirm")
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(440).MaxWidthPercent(92).
				Margin(titleBarH+8, 0, 0, 0).Padding(16, 18).Gap(12).Radius(10), true).Clip().
				Role(ui.RoleDialog).Label(locales.AiPermFullAccess())
			box.Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconTriangleAlert).FontSize(16).TextColor(pal.dangerText)
					ui.Text(c, locales.AiPermConfirmTitle()).FontSize(14).FontWeight(600).
						TextColor(pal.text).Grow(1).SingleLine()
				})
				ui.Text(c, locales.AiPermConfirmBody()).FontSize(12.5).TextColor(pal.muted).LineHeight(1.5)
				ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
					if ui.Button(c, locales.ButtonCancel()).Clicked() {
						a.post(func() {
							p.permAsk = ""
							a.invalidate()
						})
					}
					if ui.PrimaryButton(c, locales.AiPermConfirmAllow()).Clicked() {
						perm := p.permAsk
						a.post(func() {
							p.permAsk = ""
							a.setPermission(perm)
						})
					}
				})
			})
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.post(func() {
					p.permAsk = ""
					a.invalidate()
				})
			}
		})
	})
}
