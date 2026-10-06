// 与后端 /api 交互的封装。
export async function solve(input) {
  const res = await fetch("/api/solve", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = new Error("求解请求被拒绝");
    err.details = Array.isArray(data.errors)
      ? data.errors
      : [`HTTP ${res.status}`];
    throw err;
  }
  return data;
}

export async function loadExample(kind) {
  const res = await fetch(`/api/example?kind=${kind}`);
  if (!res.ok) throw new Error(`示例加载失败: HTTP ${res.status}`);
  return res.json();
}
