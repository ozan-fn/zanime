import '@videojs/react/video/skin.css';
import { VideoPlayer, VideoSkin } from '@videojs/react/video';
import { HlsJsVideo } from '@videojs/react/media/hlsjs-video';
import type { SubtitleStatus } from '../lib/types';

interface PlayerProps {
  src: string;
  subtitle?: string;
  status: SubtitleStatus | null;
}

export function Player({ src, subtitle, status }: PlayerProps) {
  const showSubtitle = Boolean(subtitle) && status?.state === 'ready';
  return (
    <div className="overflow-hidden rounded-lg border border-border bg-card shadow-2xl shadow-black/50">
      <VideoPlayer>
        <VideoSkin style={{ width: '100%', aspectRatio: '16 / 9' }}>
          <HlsJsVideo key={src} source={{ src, capRenditionToPlayerSize: false }} playsInline>
            {showSubtitle ? (
              <track kind="subtitles" src={subtitle} srcLang="id" label="Indonesia" default />
            ) : null}
          </HlsJsVideo>
        </VideoSkin>
      </VideoPlayer>
    </div>
  );
}
