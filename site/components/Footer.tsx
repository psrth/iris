import GitHubMark from "./GitHubMark";
import Mark from "./Mark";
import { GITHUB_URL } from "@/lib/site";

export default function Footer() {
  return (
    <>
      <div className="footerSpace" />
      <footer className="lane">
        <div className="footer">
          <span className="brand"><Mark size={14} /><span>IRIS · BUILT ON TAILCAT · 2026</span></span>
          <a className="ghbtn" href={GITHUB_URL} target="_blank" rel="noreferrer">
            <GitHubMark size={14} />
            <span>GITHUB</span>
          </a>
        </div>
      </footer>
    </>
  );
}
