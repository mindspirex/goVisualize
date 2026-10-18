type Context = { params: Promise<{ path: string[] }> };

async function forward(request: Request, context: Context) {
  const backend = process.env.GOVISUALIZE_API_URL ?? "http://127.0.0.1:8080";
  const { path } = await context.params;
  const endpoint = `${backend}/api/${path.map(encodeURIComponent).join("/")}`;
  try {
    const response = await fetch(endpoint, {
      method: request.method,
      headers: request.method === "POST" ? { "Content-Type": "application/json" } : undefined,
      body: request.method === "POST" ? await request.text() : undefined,
      cache: "no-store",
      signal: AbortSignal.timeout(180000),
    });
    return new Response(response.body, {
      status: response.status,
      headers: {
        "Content-Type": response.headers.get("Content-Type") ?? "application/json",
        "Cache-Control": response.headers.get("Cache-Control") ?? "no-store",
      },
    });
  } catch {
    return Response.json(
      { error: "The analysis service is unavailable. Start the Go web service and try again." },
      { status: 503 },
    );
  }
}

export async function GET(request: Request, context: Context) {
  return forward(request, context);
}

export async function POST(request: Request, context: Context) {
  return forward(request, context);
}
