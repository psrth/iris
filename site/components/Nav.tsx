import GitHubMark from "./GitHubMark";
import Mark from "./Mark";
import ThemeToggle from "./ThemeToggle";
import { GITHUB_URL } from "@/lib/site";

export default function Nav() {
  return (
    <header className="lane">
      <div className="nav">
        <div className="brand">
          <Mark />
          <span className="brandSub"><span className="brandName">IRIS</span> TRANSPORT LAYER</span>
        </div>
        <div className="navRight">
          <ThemeToggle />
          <a className="ghbtn" href={GITHUB_URL} target="_blank" rel="noreferrer">
            <GitHubMark size={14} />
            <span>GITHUB</span>
          </a>
        </div>
      </div>
    </header>
  );
}
