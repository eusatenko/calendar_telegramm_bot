"use strict";
const app = window.Telegram?.WebApp;
const el = id => document.getElementById(id);
const date = el("date");
let currentRequest;

function renderWeek() {
  const day = date.valueAsDate;
  day.setUTCDate(day.getUTCDate() - (day.getUTCDay() + 6) % 7);
  const week = el("week");
  week.replaceChildren();
  for (const label of ["Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"]) {
    const iso = day.toISOString().slice(0, 10);
    const button = document.createElement("button");
    button.textContent = label + "\n" + day.getUTCDate();
    if (iso === date.value) button.setAttribute("aria-current", "date");
    button.onclick = () => { date.value = iso; load(); };
    week.append(button);
    day.setUTCDate(day.getUTCDate() + 1);
  }
}

async function load() {
  currentRequest?.abort();
  const request = currentRequest = new AbortController();
  el("rows").replaceChildren();
  el("retry").hidden = true;
  el("status").textContent = "Загрузка расписания…";
  const params = new URLSearchParams({date: date.value, mode: el("mode").value, person: el("person").value});
  try {
    const response = await fetch("./schedule?" + params, {
      headers: {Authorization: "tma " + app.initData}, signal: request.signal, cache: "no-store"
    });
    if (request.signal.aborted) return;
    if (response.status === 401 || response.status === 403) {
      el("controls").hidden = true;
      el("status").textContent = response.status === 403 ? "Доступ к боту не предоставлен." : "Откройте календарь заново из меню бота.";
      return;
    }
    if (!response.ok) throw new Error();
    const data = await response.json();
    if (request.signal.aborted) return;
    date.value = data.date;
    renderWeek();
    for (const row of data.rows) {
      const entry = document.createElement(row.kind === "header" ? "h2" : "article");
      entry.textContent = row.text;
      if (row.location) {
        const place = document.createElement("small");
        place.textContent = row.location;
        entry.append(place);
      }
      el("rows").append(entry);
    }
    el("status").textContent = "";
    el("controls").hidden = false;
  } catch {
    if (request.signal.aborted) return;
    el("status").textContent = "Не удалось загрузить расписание.";
    el("retry").hidden = false;
  }
}

function step(days) {
  const target = date.valueAsDate;
  if (!target) return;
  target.setUTCDate(target.getUTCDate() + days);
  date.valueAsDate = target;
  load();
}
el("prev").onclick = () => step(-1);
el("next").onclick = () => step(1);
for (const input of [date, el("person"), el("mode")]) input.onchange = load;
el("retry").onclick = load;
if (app?.initData) {
  app.ready();
  app.expand();
  load();
} else {
  el("status").textContent = "Откройте «Открыть календарь» в личном чате с ботом.";
}
