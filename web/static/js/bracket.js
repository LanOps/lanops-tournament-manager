// bracket.js — bracket UI wiring: SSE refresh, CSRF injection, score-entry modal.

// Draw L-shaped SVG connectors between match cards across rounds.
// Called on load, after every HTMX swap, and on resize.
function drawBracketConnectors() {
    document.querySelectorAll('.bracket-canvas').forEach(function (canvas) {
        var old = canvas.querySelector('.bracket-svg');
        if (old) old.remove();

        if (canvas.classList.contains('no-connectors')) return;

        var rounds = canvas.querySelectorAll('.bracket-round');
        if (rounds.length < 2) return;

        var canvasRect = canvas.getBoundingClientRect();
        var scrollLeft = canvas.scrollLeft;
        var scrollTop = canvas.scrollTop;

        var svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
        svg.classList.add('bracket-svg');
        svg.setAttribute('aria-hidden', 'true');
        svg.style.cssText = 'position:absolute;top:0;left:0;pointer-events:none;overflow:visible;z-index:0;';
        svg.style.width = canvas.scrollWidth + 'px';
        svg.style.height = canvas.scrollHeight + 'px';
        canvas.appendChild(svg);

        var strokeColor = getComputedStyle(document.documentElement)
            .getPropertyValue('--text-muted').trim() || '#8b949e';

        for (var ri = 0; ri < rounds.length - 1; ri++) {
            var fromCards = rounds[ri].querySelectorAll('.match-card');
            var toCards   = rounds[ri + 1].querySelectorAll('.match-card');

            for (var ti = 0; ti < toCards.length; ti++) {
                var topCard = fromCards[ti * 2];
                var botCard = fromCards[ti * 2 + 1];
                var toCard  = toCards[ti];
                if (!topCard || !toCard) continue;

                var topRect = topCard.getBoundingClientRect();
                var toRect  = toCard.getBoundingClientRect();

                // Canvas-local coordinates (account for horizontal scroll inside canvas)
                var fromX = topRect.right  - canvasRect.left + scrollLeft;
                var toX   = toRect.left    - canvasRect.left + scrollLeft;
                var midX  = (fromX + toX) / 2;
                var topCY = topRect.top + topRect.height / 2 - canvasRect.top + scrollTop;

                var d;
                if (botCard) {
                    var botRect = botCard.getBoundingClientRect();
                    var botCY = botRect.top + botRect.height / 2 - canvasRect.top + scrollTop;
                    var midY  = (topCY + botCY) / 2;
                    // Top stub → vertical down → bottom stub, then horizontal to next card
                    d = 'M ' + fromX + ' ' + topCY +
                        ' H ' + midX  +
                        ' V ' + botCY +
                        ' M ' + fromX + ' ' + botCY +
                        ' H ' + midX  +
                        ' M ' + midX  + ' ' + midY +
                        ' H ' + toX;
                } else {
                    // Bye or single feed: direct horizontal
                    d = 'M ' + fromX + ' ' + topCY + ' H ' + toX;
                }

                var path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
                path.setAttribute('d', d);
                path.setAttribute('fill', 'none');
                path.setAttribute('stroke', strokeColor);
                path.setAttribute('stroke-width', '1');
                path.setAttribute('shape-rendering', 'crispEdges');
                svg.appendChild(path);
            }
        }
    });
}

var _connectorResizeTimer;
window.addEventListener('resize', function () {
    clearTimeout(_connectorResizeTimer);
    _connectorResizeTimer = setTimeout(drawBracketConnectors, 100);
});

document.addEventListener('DOMContentLoaded', function () {
    const csrfMeta = document.querySelector('meta[name="csrf-token"]');
    const csrfToken = csrfMeta ? csrfMeta.getAttribute('content') : '';

    // Auto-dismiss flash messages after 4s
    document.querySelectorAll('.flash').forEach(function (el) {
        setTimeout(function () {
            el.style.opacity = '0';
            setTimeout(function () { el.remove(); }, 400);
        }, 4000);
    });

    // Stamp any blank CSRF inputs on the initial render and after HTMX swaps.
    function stampCSRF() {
        document.querySelectorAll('input[name="gorilla.csrf.Token"]').forEach(function (input) {
            if (!input.value) input.value = csrfToken;
        });
    }
    stampCSRF();
    drawBracketConnectors();
    document.body.addEventListener('htmx:afterSettle', function () {
        stampCSRF();
        drawBracketConnectors();
    });

    // --- Score-entry modal ---
    const modal = document.getElementById('score-modal');
    if (!modal) return; // no modal on pages where the user can't submit

    const form = modal.querySelector('#score-form');
    const radioA = modal.querySelector('input[name="winner_id"][data-side="a"]');
    const radioB = modal.querySelector('input[name="winner_id"][data-side="b"]');
    const nameA = modal.querySelector('[data-a-name]');
    const nameB = modal.querySelector('[data-b-name]');
    const scoreA = modal.querySelector('input[name="score_a"]');
    const scoreB = modal.querySelector('input[name="score_b"]');

    // Remember whether the user has explicitly touched the winner picker. If not,
    // we keep auto-defaulting to the higher scorer as they type.
    let userPickedWinner = false;

    const modalTitle = modal.querySelector('#score-modal-title');

    function openFor(card) {
        form.action = '/matches/' + card.dataset.matchId + '/result';
        radioA.value = card.dataset.aId;
        radioB.value = card.dataset.bId;
        nameA.textContent = card.dataset.aName;
        nameB.textContent = card.dataset.bName;

        const editMode = card.dataset.mode === 'edit';
        if (modalTitle) modalTitle.textContent = editMode ? 'Edit result' : 'Enter result';

        if (editMode) {
            // Pre-fill stored scores and winner; let the user override.
            scoreA.value = card.dataset.aScore || '';
            scoreB.value = card.dataset.bScore || '';
            const winner = card.dataset.winnerId;
            radioA.checked = winner && winner === radioA.value;
            radioB.checked = winner && winner === radioB.value;
            // Treat the pre-filled winner as an explicit pick so typing a
            // new score doesn't flip the selection until the user themselves
            // clicks a radio again.
            userPickedWinner = Boolean(winner);
        } else {
            scoreA.value = '';
            scoreB.value = '';
            radioA.checked = false;
            radioB.checked = false;
            userPickedWinner = false;
        }

        modal.hidden = false;
        modal.setAttribute('aria-hidden', 'false');
        document.body.classList.add('modal-open');
        scoreA.focus();
    }

    function close() {
        modal.hidden = true;
        modal.setAttribute('aria-hidden', 'true');
        document.body.classList.remove('modal-open');
    }

    // Auto-pick the higher-scored player as winner, unless the user has chosen.
    function autoPickWinner() {
        if (userPickedWinner) return;
        const a = parseInt(scoreA.value, 10);
        const b = parseInt(scoreB.value, 10);
        if (isNaN(a) && isNaN(b)) {
            radioA.checked = false; radioB.checked = false;
            return;
        }
        const aVal = isNaN(a) ? -Infinity : a;
        const bVal = isNaN(b) ? -Infinity : b;
        if (aVal > bVal) { radioA.checked = true; radioB.checked = false; }
        else if (bVal > aVal) { radioB.checked = true; radioA.checked = false; }
        // ties leave both unchecked — user must pick
    }

    scoreA.addEventListener('input', autoPickWinner);
    scoreB.addEventListener('input', autoPickWinner);

    [radioA, radioB].forEach(function (r) {
        r.addEventListener('change', function () { userPickedWinner = true; });
    });

    // Click-to-open: delegate on the document so HTMX swaps keep working.
    document.addEventListener('click', function (e) {
        // Close buttons inside the modal
        if (e.target.closest('[data-modal-close]')) {
            close();
            return;
        }
        // Backdrop click closes (target IS the backdrop, not a child)
        if (e.target === modal) { close(); return; }
        // Match card
        const card = e.target.closest('.match-card[data-submittable="true"]');
        if (card) openFor(card);
    });

    // Keyboard activation on focused card + Esc to close
    document.addEventListener('keydown', function (e) {
        if (e.key === 'Escape' && !modal.hidden) { close(); return; }
        if ((e.key === 'Enter' || e.key === ' ') && document.activeElement
            && document.activeElement.matches('.match-card[data-submittable="true"]')) {
            e.preventDefault();
            openFor(document.activeElement);
        }
    });

    // Before submit: refuse if neither radio is checked (required= alone doesn't
    // always fire on radio groups where the value changed programmatically).
    // Submit via fetch instead of letting the browser navigate. Backend still
    // redirects (303), but fetch swallows that; we just need a 2xx on the
    // final response. The bracket updates in place via the existing SSE
    // broadcast so there's nothing else for us to re-render by hand.
    form.addEventListener('submit', async function (e) {
        e.preventDefault();
        if (!radioA.checked && !radioB.checked) {
            scoreA.focus();
            alert('Pick a winner.');
            return;
        }
        const submitBtn = form.querySelector('button[type="submit"]');
        const prevLabel = submitBtn.textContent;
        submitBtn.disabled = true;
        submitBtn.textContent = 'Saving…';
        try {
            const resp = await fetch(form.action, {
                method: 'POST',
                body: new FormData(form),
                credentials: 'same-origin',
                headers: { 'Accept': 'text/html' },
            });
            if (!resp.ok) {
                const body = await resp.text();
                alert('Submit failed: ' + (body.slice(0, 300) || resp.status));
                return;
            }
            close();
        } catch (err) {
            alert('Network error: ' + err.message);
        } finally {
            submitBtn.disabled = false;
            submitBtn.textContent = prevLabel;
        }
    });
});
