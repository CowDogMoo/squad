package main

import (
	"context"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/cowdogmoo/squad/logging"
)

// attachIndicatorJS frames the controlled page in orange and pins a "squad"
// badge to it — the in-page equivalent of the tab decoration browser
// extensions use to show which tab an agent is driving. Idempotent, and
// pointer-events:none keeps the page fully interactive. Injected both into
// the current document and (via Page.addScriptToEvaluateOnNewDocument) every
// future navigation while the server stays attached.
const attachIndicatorJS = `(() => {
	const install = () => {
		if (document.getElementById('__squad_indicator__')) return;
		const style = document.createElement('style');
		style.id = '__squad_indicator_style__';
		style.textContent = ` + "`" + `
			#__squad_indicator__ { position: fixed; inset: 0; border: 3px solid #e8956b; pointer-events: none; z-index: 2147483647; }
			#__squad_indicator__ .__squad_badge__ { position: absolute; top: 0; left: 50%; transform: translateX(-50%); background: #e8956b; color: #1f1f1f; font: 600 12px/1.6 system-ui, sans-serif; padding: 1px 12px; border-radius: 0 0 8px 8px; }
		` + "`" + `;
		const frame = document.createElement('div');
		frame.id = '__squad_indicator__';
		const badge = document.createElement('div');
		badge.className = '__squad_badge__';
		badge.textContent = 'squad';
		frame.appendChild(badge);
		document.documentElement.appendChild(style);
		document.documentElement.appendChild(frame);
	};
	if (document.documentElement) { install(); } else { addEventListener('DOMContentLoaded', install); }
	return true;
})()`

// removeIndicatorJS undoes attachIndicatorJS on the current document.
const removeIndicatorJS = `(() => {
	document.getElementById('__squad_indicator__')?.remove();
	document.getElementById('__squad_indicator_style__')?.remove();
	return true;
})()`

// installAttachIndicator marks the attached page as agent-controlled and
// returns a remove func for detach time. Installation failure is fatal to
// the attach: the whole point of driving a user's real browser is that they
// can see it happening.
func installAttachIndicator(tabCtx context.Context) (func(), error) {
	var scriptID page.ScriptIdentifier
	err := chromedp.Run(tabCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			id, err := page.AddScriptToEvaluateOnNewDocument(attachIndicatorJS).Do(ctx)
			if err != nil {
				return err
			}
			scriptID = id
			return nil
		}),
		chromedp.Evaluate(attachIndicatorJS, nil),
	)
	if err != nil {
		return nil, err
	}

	remove := func() {
		// Best-effort: the session (or the whole browser) may already be
		// gone by detach time, and detaching must not hang on it.
		rmCtx, cancel := context.WithTimeout(tabCtx, 5*time.Second)
		defer cancel()
		err := chromedp.Run(rmCtx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				return page.RemoveScriptToEvaluateOnNewDocument(scriptID).Do(ctx)
			}),
			chromedp.Evaluate(removeIndicatorJS, nil),
		)
		if err != nil {
			logging.Debug("squad browser: remove attach indicator: %v", err)
		}
	}
	return remove, nil
}
