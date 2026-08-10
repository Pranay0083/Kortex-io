import { afterEach, describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import Hop from "@/pages/Hop";
import { mockApi } from "@/test/mockApi";

const renderHop = (code) =>
  render(
    <MemoryRouter initialEntries={[`/${code}`]}>
      <Routes>
        <Route path="/:code" element={<Hop />} />
      </Routes>
    </MemoryRouter>,
  );

describe("Hop", () => {
  let calls;
  afterEach(() => calls?.restore());

  it("shows a not-found page for unknown codes instead of redirecting", async () => {
    calls = mockApi({ "GET /resolve/nope": [404, { detail: "unknown short code" }] });
    renderHop("nope");
    expect(await screen.findByText("Link not found.")).toBeInTheDocument();
    expect(screen.getByTestId("not-found-page")).toHaveTextContent("/nope");
  });

  it("explains disabled links", async () => {
    calls = mockApi({ "GET /resolve/off": [200, { long_url: "https://x.test", active: false }] });
    renderHop("off");
    expect(await screen.findByText("Link disabled.")).toBeInTheDocument();
  });

  it("explains expired links", async () => {
    calls = mockApi({
      "GET /resolve/old": [200, { long_url: "https://x.test", active: true, expires_at: 1 }],
    });
    renderHop("old");
    expect(await screen.findByText("Link expired.")).toBeInTheDocument();
  });
});
