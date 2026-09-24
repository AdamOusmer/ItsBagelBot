// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

let typedText = "";
const targetWord = "bagel";
let isBagelMode = false;
let lastSeedTime = 0;

document.addEventListener("keydown", (e) => {
    if (e.key.length !== 1) return;

    typedText += e.key.toLowerCase();

    if (typedText.length > targetWord.length) {
        typedText = typedText.slice(-targetWord.length);
    }

    if (typedText === targetWord) {
        triggerBagelMode();
    }
});

document.addEventListener("astro:before-preparation", cleanupBagelMode);

function cleanupBagelMode() {
    isBagelMode = false;

    document.body.classList.remove("bagel-cursor");

    document.removeEventListener("mousemove", spawnSesameSeed);

    document.querySelectorAll(".sesame-seed, .bagel-drop").forEach((el) => el.remove());
}

function triggerBagelMode() {
    isBagelMode = true;

    document.body.classList.add("bagel-cursor");

    rainBagels();

    document.addEventListener("mousemove", spawnSesameSeed);
}

function rainBagels() {
    const numBagels = 30;
    for (let i = 0; i < numBagels; i++) {
        setTimeout(() => {
            const bagel = document.createElement("div");
            bagel.classList.add("bagel-drop");
            bagel.innerText = "🥯";

            bagel.style.left = Math.random() * 100 + "vw";

            const duration = 2 + Math.random() * 2;
            bagel.style.animationDuration = `${duration}s`;

            const size = 1.5 + Math.random() * 2;
            bagel.style.fontSize = `${size}rem`;

            document.body.appendChild(bagel);

            setTimeout(() => {
                bagel.remove();
            }, duration * 1000);
        }, i * 100);
    }
}

function spawnSesameSeed(e) {
    if (!isBagelMode) return;

    const now = Date.now();
    if (now - lastSeedTime < 50) return;
    lastSeedTime = now;

    const seed = document.createElement("div");
    seed.classList.add("sesame-seed");

    seed.style.left = e.clientX + "px";
    seed.style.top = e.clientY + "px";

    const rot = Math.random() * 360;
    seed.style.setProperty("--rot", `${rot}deg`);

    document.body.appendChild(seed);

    setTimeout(() => {
        seed.remove();
    }, 1000);
}