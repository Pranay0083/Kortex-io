import { afterEach, describe, expect, it } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { Toaster } from "sonner";
import Links from "@/pages/Links";
import { countCalls, mockApi } from "@/test/mockApi";

const CODE = "Ab3dE6gH9jK";

const shortenResponse = {
  link: { code: CODE, long_url: "https://example.com/very/long", shard: 2, active: true },
  short_url: `/api/r/${CODE}`, // relative on purpose: the UI must not rely on it
  pretty_url: `/${CODE}`,
  timings: { id_gen_us: 1, pg_write_us: 900, redis_us: 120, shard: 2 },
  snowflake: {
    id: "123",
    time_bits: "0101",
    node_bits: "01",
    seq_bits: "001",
    timestamp_ms: Date.now(),
    node: 1,
    sequence: 0,
  },
};

const row = (i, extra = {}) => ({
  link: {
    code: `code${String(i).padStart(7, "0")}`,
    title: `Title ${i}`,
    long_url: `https://site${i}.example.com/`,
    shard: i % 4,
    active: true,
    ...extra,
  },
  clicks_total: i,
  clicks_24h: 0,
  last_seen: 0,
});

const renderLinks = () =>
  render(
    <MemoryRouter>
      <Toaster />
      <Links />
    </MemoryRouter>,
  );

describe("Links page", () => {
  let calls;
  afterEach(() => calls?.restore());

  it("create flow shows an absolute short URL and QR", async () => {
    calls = mockApi({
      "GET /links": [200, { items: [] }],
      "GET /system/stats": [200, { shard_count: 4, node_name: "kortex-test" }],
      "POST /links": [201, shortenResponse],
    });
    const user = userEvent.setup();
    renderLinks();
    await screen.findByText(/no links yet/i);

    await user.type(screen.getByLabelText("Destination URL"), "https://example.com/very/long");
    await user.click(screen.getByTestId("shorten-submit"));

    const shown = await screen.findByTestId("created-short-url");
    expect(shown).toHaveTextContent(`${window.location.origin}/${CODE}`);
    expect(shown.textContent).toMatch(/^https?:\/\//);
    expect(screen.getByTestId("created-qr").getAttribute("src")).toMatch(
      new RegExp(`^${window.location.origin}/api/qr/${CODE}\\?url=http`),
    );
    expect(screen.getByTestId("open-created-url")).toHaveAttribute(
      "href",
      `${window.location.origin}/${CODE}`,
    );
    const body = JSON.parse(calls.find((c) => c.key === "POST /links").data);
    expect(body).toMatchObject({ url: "https://example.com/very/long", permanent: false });
  });

  it("row menu closes on Escape and on an outside click", async () => {
    calls = mockApi({
      "GET /links": [200, { items: [row(1)] }],
      "GET /system/stats": [200, {}],
    });
    const user = userEvent.setup();
    renderLinks();
    const menuBtn = await screen.findByTestId("menu-code0000001");
    await user.click(menuBtn);
    expect(screen.getByRole("menu")).toBeInTheDocument();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(menuBtn).toHaveFocus();

    await user.click(menuBtn);
    expect(screen.getByRole("menu")).toBeInTheDocument();
    await user.click(screen.getByTestId("links-page"));
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });

  it("sends permanent: true when the 301 checkbox is ticked", async () => {
    calls = mockApi({
      "GET /links": [200, { items: [] }],
      "GET /system/stats": [200, {}],
      "POST /links": [201, shortenResponse],
    });
    const user = userEvent.setup();
    renderLinks();
    await screen.findByText(/no links yet/i);
    const box = screen.getByLabelText("Permanent redirect (301)");
    expect(box).toBe(screen.getByTestId("permanent-toggle"));
    expect(box).not.toBeChecked();
    await user.click(box);
    expect(box).toBeChecked();
    await user.type(screen.getByLabelText("Destination URL"), "https://example.com/x");
    await user.click(screen.getByTestId("shorten-submit"));
    await screen.findByTestId("created-short-url");
    const body = JSON.parse(calls.find((c) => c.key === "POST /links").data);
    expect(body).toMatchObject({ permanent: true });
  });

  it("searches and paginates client-side", async () => {
    const items = Array.from({ length: 30 }, (_, i) => row(i));
    calls = mockApi({
      "GET /links": [200, { items }],
      "GET /system/stats": [200, {}],
    });
    const user = userEvent.setup();
    renderLinks();
    await screen.findByTestId("link-row-code0000000");
    expect(screen.getAllByTestId(/^link-row-/)).toHaveLength(25);
    expect(screen.getByTestId("links-page-info")).toHaveTextContent("1–25 of 30");

    await user.click(screen.getByTestId("links-next"));
    expect(screen.getAllByTestId(/^link-row-/)).toHaveLength(5);

    await user.type(screen.getByTestId("links-search"), "site17.");
    expect(screen.getAllByTestId(/^link-row-/)).toHaveLength(1);
    expect(screen.getByTestId("link-row-code0000017")).toBeInTheDocument();
  });

  it("asks for confirmation before deleting and surfaces a 403", async () => {
    calls = mockApi({
      "GET /links": [200, { items: [row(1)] }],
      "GET /system/stats": [200, {}],
      "DELETE /links/code0000001": [403, { detail: "demo links are protected" }],
    });
    const user = userEvent.setup();
    renderLinks();
    const menuBtn = await screen.findByRole("button", { name: "More actions for /code0000001" });
    expect(menuBtn).toHaveAttribute("aria-expanded", "false");
    await user.click(menuBtn);
    expect(menuBtn).toHaveAttribute("aria-expanded", "true");
    const menu = screen.getByRole("menu");
    expect(within(menu).getByTestId("qr-code0000001")).toBeInTheDocument();
    expect(within(menu).getByTestId("toggle-code0000001")).toHaveTextContent("Disable link");
    await user.click(within(menu).getByRole("menuitem", { name: "Delete" }));
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(countCalls(calls, "DELETE /links/code0000001")).toBe(0);

    const dialog = screen.getByTestId("confirm-dialog");
    await user.click(within(dialog).getByTestId("confirm-ok"));
    await waitFor(() => expect(countCalls(calls, "DELETE /links/code0000001")).toBe(1));
    expect(await screen.findByText("demo links are protected")).toBeInTheDocument();
  });
});
