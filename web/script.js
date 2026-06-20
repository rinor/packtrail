// Packtrail site — scroll reveal + copy-to-clipboard. No dependencies.
(function () {
  "use strict";

  // Reveal-on-scroll
  const reveals = document.querySelectorAll(".reveal");
  if ("IntersectionObserver" in window && reveals.length) {
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (e.isIntersecting) {
            e.target.classList.add("in");
            io.unobserve(e.target);
          }
        }
      },
      { threshold: 0.1, rootMargin: "0px 0px -6% 0px" }
    );
    reveals.forEach((el) => io.observe(el));
  } else {
    reveals.forEach((el) => el.classList.add("in"));
  }

  // Docs sidebar scroll-spy — highlight the section currently in view.
  // No-op on pages without a .docs__nav (e.g. the landing page).
  const docNav = document.querySelector(".docs__nav");
  if (docNav && "IntersectionObserver" in window) {
    const links = new Map(
      Array.from(docNav.querySelectorAll("a[href^='#']")).map((a) => [
        a.getAttribute("href").slice(1),
        a,
      ])
    );
    const spy = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (e.isIntersecting) {
            links.forEach((a) => a.classList.remove("active"));
            const active = links.get(e.target.id);
            if (active) active.classList.add("active");
          }
        }
      },
      { rootMargin: "-80px 0px -70% 0px", threshold: 0 }
    );
    document.querySelectorAll(".docs__body > section[id]").forEach((s) => spy.observe(s));
  }

  // Copy buttons — strip prompt markers ("$ ") so pasted commands run clean.
  document.querySelectorAll(".copy").forEach((btn) => {
    btn.addEventListener("click", async () => {
      const target = document.querySelector(btn.dataset.copy);
      if (!target) return;
      let text = target.innerText.replace(/^\$\s/gm, "");
      try {
        await navigator.clipboard.writeText(text);
      } catch {
        const ta = document.createElement("textarea");
        ta.value = text;
        document.body.appendChild(ta);
        ta.select();
        document.execCommand("copy");
        ta.remove();
      }
      const original = btn.textContent;
      btn.textContent = "Copied";
      btn.classList.add("copied");
      setTimeout(() => {
        btn.textContent = original;
        btn.classList.remove("copied");
      }, 1600);
    });
  });
})();
