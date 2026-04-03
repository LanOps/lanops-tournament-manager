// bracket.js — SSE reconnect + bracket rendering helpers

// HTMX SSE extension handles reconnect automatically.
// This file adds minimal helpers for UX polish.

document.addEventListener('DOMContentLoaded', function () {
    // Auto-dismiss flash messages after 4s
    const flashes = document.querySelectorAll('.flash');
    flashes.forEach(function (el) {
        setTimeout(function () {
            el.style.opacity = '0';
            setTimeout(function () { el.remove(); }, 400);
        }, 4000);
    });

    // Inject CSRF token into all forms dynamically
    // The token is embedded in a meta tag by the server
    const csrfMeta = document.querySelector('meta[name="csrf-token"]');
    if (csrfMeta) {
        const token = csrfMeta.getAttribute('content');
        document.querySelectorAll('input[name="gorilla.csrf.Token"]').forEach(function (input) {
            input.value = token;
        });
    }

    // Re-inject CSRF into dynamically loaded content
    document.body.addEventListener('htmx:afterSettle', function () {
        if (csrfMeta) {
            const token = csrfMeta.getAttribute('content');
            document.querySelectorAll('input[name="gorilla.csrf.Token"]').forEach(function (input) {
                if (!input.value) input.value = token;
            });
        }
    });
});
