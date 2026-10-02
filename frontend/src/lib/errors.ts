/** Plain-language explanations for the connection failures people hit most,
    each kept next to the original text so nothing is hidden from a bug report. */
const RULES: [RegExp, string][] = [
  [/HOST KEY CHANGED/i, "The server's identity has changed since you last connected. Check with your provider before trusting it."],
  [/rejected by user/i, "You declined to trust the server."],
  [/first record does not look like a TLS handshake/i, "This port is not speaking FTPS. Try SFTP, or the other FTPS mode."],
  [/unable to authenticate|permission denied|login incorrect|\b530\b/i, "The server rejected the login. Check the username and password, or the key."],
  [/no such host|server misbehaving/i, "That server name could not be found. Check the spelling and your internet connection."],
  [/connection refused/i, "The server refused the connection. Check the port, and that this is the right protocol."],
  [/i\/o timeout|timed out|deadline exceeded/i, "The server did not answer in time. Check the address and port, and that nothing is blocking the connection."],
  [/handshake failed|no common algorithm|ssh: .*(EOF|reset)|protocol error/i, "The server did not respond like an SSH server on this port. If it is an FTP server, choose FTPS."],
];

export function friendlyError(err: unknown): string {
  const raw = String(err).replace(/^Error:\s*/, "");
  const hit = RULES.find(([re]) => re.test(raw));
  return hit ? `${hit[1]} (${raw})` : raw;
}
