document.addEventListener("DOMContentLoaded", () => {
    initPageEnter();
    initOnboarding();
    initTilt();
    initMagnetic();
    initReveal();
});

// Re-initialize DOM-dependent effects after HTMX swaps
document.addEventListener('htmx:afterSwap', function(evt) {
    initTilt();
    initMagnetic();
    initReveal();
});

// ─── Page-enter stagger animation ────────────────────────────────────────────

function initPageEnter() {
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    if (reduced) return;

    // Animate direct children of .main-content that are not already .reveal
    // (reveal elements are handled by IntersectionObserver in initReveal)
    const container = document.querySelector('.main-content');
    if (!container) return;

    const targets = Array.from(container.children).filter(el => !el.classList.contains('reveal'));
    if (!targets.length) return;

    targets.forEach((el, i) => {
        el.animate(
            [
                { opacity: 0, transform: 'translateY(18px)' },
                { opacity: 1, transform: 'translateY(0)' }
            ],
            {
                duration: 380,
                delay: i * 60,
                easing: 'cubic-bezier(0.22, 1, 0.36, 1)',
                fill: 'backwards'
            }
        );
    });
}

// ─── Onboarding stepper ───────────────────────────────────────────────────────

function initOnboarding() {
    const track = document.getElementById('slider-track');
    if (!track) return;

    const panels = document.querySelectorAll('.step-panel');
    const progressBtns = document.querySelectorAll('.onboard-progress button');
    let currentStep = 0;

    function goToStep(index) {
        if (index < 0 || index >= panels.length) return;
        currentStep = index;

        // Spring-feel via CSS transition on track (set in stylesheet as
        // transition: transform 0.45s cubic-bezier(0.34,1.56,0.64,1))
        track.style.transform = `translateX(-${currentStep * 33.3333}%)`;

        panels.forEach((p, i) => {
            p.classList.toggle('active', i === currentStep);
        });

        progressBtns.forEach((btn, i) => {
            btn.classList.toggle('active', i === currentStep);
        });
    }

    // [data-next] buttons — advance one step (skip if on last step)
    document.querySelectorAll('[data-next]').forEach(btn => {
        btn.addEventListener('click', () => {
            // Do not auto-advance past the final step (sign-in)
            if (currentStep < panels.length - 1) goToStep(currentStep + 1);
        });
    });

    // [data-goto] buttons — jump to arbitrary step
    document.querySelectorAll('[data-goto]').forEach(btn => {
        // Keyboard accessibility: Enter/Space trigger the same action as click
        btn.addEventListener('click', handleGoto);
        btn.addEventListener('keydown', (e) => {
            if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                handleGoto(e);
            }
        });
    });

    function handleGoto(e) {
        const target = parseInt(e.currentTarget.getAttribute('data-goto'), 10);
        if (!isNaN(target)) goToStep(target);
    }
}

// ─── Commands panel ───────────────────────────────────────────────────────────
// Selection is now server-driven via htmx (hx-get on .cmd-item).
// No client-side dummy logic needed.

// ─── Tilt / specular (data-tilt) ─────────────────────────────────────────────

function initTilt() {
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

    document.querySelectorAll('[data-tilt]').forEach(el => {
        // Guard against double-binding
        if (el.__tiltBound) return;
        el.__tiltBound = true;

        let rafId = null;
        let pendingMx = 0.5;
        let pendingMy = 0.5;

        function applyTilt() {
            rafId = null;
            el.style.setProperty('--mx', pendingMx);
            el.style.setProperty('--my', pendingMy);

            if (!reduced) {
                const rx = (pendingMy - 0.5) * -10;  // rotateX: tilt up/down
                const ry = (pendingMx - 0.5) * 10;   // rotateY: tilt left/right
                el.style.transform = `perspective(600px) rotateX(${rx}deg) rotateY(${ry}deg) scale3d(1.02,1.02,1.02)`;
            }
        }

        function onPointerMove(e) {
            const rect = el.getBoundingClientRect();
            pendingMx = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
            pendingMy = Math.max(0, Math.min(1, (e.clientY - rect.top) / rect.height));

            if (rafId === null) {
                rafId = requestAnimationFrame(applyTilt);
            }
        }

        function onPointerLeave() {
            if (rafId !== null) {
                cancelAnimationFrame(rafId);
                rafId = null;
            }
            // Smooth reset: animate back to neutral
            el.style.transition = 'transform 0.5s cubic-bezier(0.23,1,0.32,1)';
            el.style.transform = '';
            el.style.setProperty('--mx', '0.5');
            el.style.setProperty('--my', '0.5');
            el.addEventListener('transitionend', function clearTransition() {
                el.style.transition = '';
                el.removeEventListener('transitionend', clearTransition);
            }, { once: true });
        }

        el.style.willChange = 'transform';
        el.addEventListener('pointermove', onPointerMove, { passive: true });
        el.addEventListener('pointerleave', onPointerLeave);
    });
}

// ─── Magnetic buttons (data-magnetic) ────────────────────────────────────────

function initMagnetic() {
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

    document.querySelectorAll('[data-magnetic]').forEach(el => {
        if (el.__magneticBound) return;
        el.__magneticBound = true;

        const strength = parseFloat(el.dataset.magnetic) || 0.35;

        function onPointerMove(e) {
            if (reduced) return;
            const rect = el.getBoundingClientRect();
            const cx = rect.left + rect.width / 2;
            const cy = rect.top + rect.height / 2;
            const dx = (e.clientX - cx) * strength;
            const dy = (e.clientY - cy) * strength;
            el.style.transform = `translate(${dx}px, ${dy}px)`;
        }

        function onPointerLeave() {
            if (reduced) return;
            // Spring-back via Web Animations API
            const current = new DOMMatrix(getComputedStyle(el).transform);
            el.style.transform = '';
            el.animate(
                [
                    { transform: `translate(${current.m41}px, ${current.m42}px)` },
                    { transform: 'translate(0px, 0px)' }
                ],
                {
                    duration: 600,
                    easing: 'cubic-bezier(0.34, 1.56, 0.64, 1)',
                    fill: 'forwards'
                }
            );
        }

        el.addEventListener('pointermove', onPointerMove, { passive: true });
        el.addEventListener('pointerleave', onPointerLeave);
    });
}

// ─── Reveal on scroll (.reveal → .in-view) ────────────────────────────────────

function initReveal() {
    const els = document.querySelectorAll('.reveal:not(.in-view)');
    if (!els.length) return;

    const observer = new IntersectionObserver((entries) => {
        entries.forEach(entry => {
            if (entry.isIntersecting) {
                entry.target.classList.add('in-view');
                observer.unobserve(entry.target);
            }
        });
    }, { threshold: 0.12 });

    els.forEach(el => observer.observe(el));
}
