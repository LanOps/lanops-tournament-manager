// bracket.js — bracket UI wiring: SSE refresh, CSRF injection, score-entry modal.

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
    document.body.addEventListener('htmx:afterSettle', stampCSRF);

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

    function openFor(card) {
        form.action = '/matches/' + card.dataset.matchId + '/result';
        radioA.value = card.dataset.aId;
        radioB.value = card.dataset.bId;
        nameA.textContent = card.dataset.aName;
        nameB.textContent = card.dataset.bName;
        scoreA.value = '';
        scoreB.value = '';
        radioA.checked = false;
        radioB.checked = false;
        userPickedWinner = false;

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
    form.addEventListener('submit', function (e) {
        if (!radioA.checked && !radioB.checked) {
            e.preventDefault();
            scoreA.focus();
            alert('Pick a winner.');
        }
    });
});
