import "./markdown-it.js";

const markdownItRuntime = globalThis.markdownit;
if (typeof markdownItRuntime !== "function") {
  throw new Error("markdown-it runtime is missing");
}

export default markdownItRuntime;
