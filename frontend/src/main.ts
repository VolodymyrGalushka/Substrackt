import { mount } from "svelte";
import "./app.css";
import { initTheme } from "./lib/theme.svelte";
import App from "./App.svelte";

initTheme();
mount(App, { target: document.getElementById("app")! });
