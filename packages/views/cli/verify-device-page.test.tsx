import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// --- mocks --------------------------------------------------------------
// We mock the api client wholesale so the view is tested against a
// deterministic backend. The real contract is covered by handler tests.
const mockVerify = vi.hoisted(() => vi.fn());
const mockApprove = vi.hoisted(() => vi.fn());
const mockDeny = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    verifyCliDevice: mockVerify,
    approveCliDevice: mockApprove,
    denyCliDevice: mockDeny,
  },
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

// Import AFTER mocks so the module resolves against them.
import { VerifyDevicePage } from "./verify-device-page";

const VERIFIED = {
  hostname: "alice-macbook",
  requested_at: "2026-04-22T10:00:00Z",
  expires_at: "2026-04-22T10:10:00Z",
};

describe("VerifyDevicePage", () => {
  beforeEach(() => {
    mockVerify.mockReset();
    mockApprove.mockReset();
    mockDeny.mockReset();
  });

  it("auto-verifies when a code is prefilled from the CLI link", async () => {
    mockVerify.mockResolvedValueOnce(VERIFIED);
    render(<VerifyDevicePage initialCode="ABCD-EFGH" />);

    await waitFor(() => {
      expect(mockVerify).toHaveBeenCalledWith("ABCD-EFGH");
    });
    // Confirm view shows the hostname returned by /verify
    expect(
      await screen.findByText(/alice-macbook/),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /authorize/i })).toBeEnabled();
  });

  it("shows the manual input form when no code is supplied", () => {
    render(<VerifyDevicePage />);
    expect(screen.getByLabelText(/device code/i)).toBeInTheDocument();
    // Verify call must NOT fire until the user clicks Continue.
    expect(mockVerify).not.toHaveBeenCalled();
  });

  it("submits a manually entered code", async () => {
    mockVerify.mockResolvedValueOnce(VERIFIED);
    const user = userEvent.setup();
    render(<VerifyDevicePage />);

    await user.type(screen.getByLabelText(/device code/i), "wxyz-1234");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => {
      expect(mockVerify).toHaveBeenCalledWith("wxyz-1234");
    });
    expect(
      await screen.findByText(/alice-macbook/),
    ).toBeInTheDocument();
  });

  it("maps a 404/invalid verify error to a user-facing message and returns to input", async () => {
    mockVerify.mockRejectedValueOnce(new Error("404 not found"));
    const user = userEvent.setup();
    render(<VerifyDevicePage />);

    await user.type(screen.getByLabelText(/device code/i), "abcd-efgh");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    expect(
      await screen.findByText(/not valid or has already been used/i),
    ).toBeInTheDocument();
    // Input form is still mounted so the user can retry.
    expect(screen.getByLabelText(/device code/i)).toBeInTheDocument();
  });

  it("maps an expired code to the expired message", async () => {
    mockVerify.mockRejectedValueOnce(new Error("410 expired"));
    render(<VerifyDevicePage initialCode="XXXX-XXXX" />);

    expect(
      await screen.findByText(/expired/i),
    ).toBeInTheDocument();
  });

  it("approves when Authorize is clicked and shows the success screen", async () => {
    mockVerify.mockResolvedValueOnce(VERIFIED);
    mockApprove.mockResolvedValueOnce(undefined);
    render(<VerifyDevicePage initialCode="ABCD-EFGH" />);

    const approve = await screen.findByRole("button", { name: /authorize/i });
    fireEvent.click(approve);

    await waitFor(() => {
      expect(mockApprove).toHaveBeenCalledWith("ABCD-EFGH");
    });
    expect(await screen.findByText(/device authorized/i)).toBeInTheDocument();
    // Close-the-tab copy is part of the terminal success state.
    expect(
      screen.getByText(/close this tab and return to your terminal/i),
    ).toBeInTheDocument();
    expect(mockDeny).not.toHaveBeenCalled();
  });

  it("denies when the 'this wasn't me' button is clicked", async () => {
    mockVerify.mockResolvedValueOnce(VERIFIED);
    mockDeny.mockResolvedValueOnce(undefined);
    render(<VerifyDevicePage initialCode="ABCD-EFGH" />);

    const denyBtn = await screen.findByRole("button", {
      name: /this wasn't me/i,
    });
    fireEvent.click(denyBtn);

    await waitFor(() => {
      expect(mockDeny).toHaveBeenCalledWith("ABCD-EFGH");
    });
    expect(
      await screen.findByText(/authorization denied/i),
    ).toBeInTheDocument();
    expect(mockApprove).not.toHaveBeenCalled();
  });

  it("stays on the confirm view if approve fails", async () => {
    mockVerify.mockResolvedValueOnce(VERIFIED);
    mockApprove.mockRejectedValueOnce(new Error("500 server error"));
    render(<VerifyDevicePage initialCode="ABCD-EFGH" />);

    const approve = await screen.findByRole("button", { name: /authorize/i });
    fireEvent.click(approve);

    // Error surface is inline under the buttons.
    expect(
      await screen.findByText(/something went wrong/i),
    ).toBeInTheDocument();
    // User can still retry or deny.
    expect(screen.getByRole("button", { name: /authorize/i })).toBeEnabled();
    expect(screen.getByRole("button", { name: /this wasn't me/i })).toBeEnabled();
  });
});
