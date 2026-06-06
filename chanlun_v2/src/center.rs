use serde::{Deserialize, Serialize};
use crate::segment::Segment;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Center {
    pub id: usize,
    pub zg: f64,
    pub zd: f64,
    pub high: f64,
    pub low: f64,
    pub segments: Vec<usize>,
    pub start_time: i64,
    pub end_time: i64,
}

/// 从线段序列识别中枢
/// 规则：至少3个连续线段的价格区间有重叠形成中枢；后续段若仍在ZG-ZD内则扩展中枢
pub fn build_centers(segments: &[Segment]) -> Vec<Center> {
    if segments.len() < 3 {
        return vec![];
    }
    let mut centers: Vec<Center> = Vec::new();
    let mut i = 0;
    let mut center_id = 0;

    while i + 2 < segments.len() {
        // 尝试用 segments[i], [i+1], [i+2] 构建中枢
        let zg = segments[i].high.min(segments[i + 1].high).min(segments[i + 2].high);
        let zd = segments[i].low.max(segments[i + 1].low).max(segments[i + 2].low);

        if zg <= zd {
            i += 1;
            continue;
        }

        // 中枢成立，尝试扩展
        let mut seg_ids = vec![segments[i].id, segments[i + 1].id, segments[i + 2].id];
        let mut high = segments[i].high.max(segments[i + 1].high).max(segments[i + 2].high);
        let mut low = segments[i].low.min(segments[i + 1].low).min(segments[i + 2].low);
        let start_time = segments[i].start_time;
        let mut end_time = segments[i + 2].end_time;
        let mut j = i + 3;

        // 扩展：后续段若与中枢区间有交集则纳入
        while j < segments.len() {
            let s = &segments[j];
            if s.high > zd && s.low < zg {
                // 与中枢有交集
                seg_ids.push(s.id);
                high = high.max(s.high);
                low = low.min(s.low);
                end_time = s.end_time;
                j += 1;
            } else {
                break;
            }
        }

        centers.push(Center {
            id: center_id,
            zg,
            zd,
            high,
            low,
            segments: seg_ids,
            start_time,
            end_time,
        });
        center_id += 1;
        i = j;
    }
    centers
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::kline::Direction;
    use crate::stroke::Stroke;
    use crate::fractal::{Fractal, FractalType};

    fn seg(id: usize, dir: Direction, high: f64, low: f64) -> Segment {
        Segment {
            id, direction: dir, high, low, start_time: id as i64, end_time: (id + 1) as i64,
            strokes: vec![Stroke {
                id: 0, direction: dir, high, low,
                start: Fractal { fx_type: FractalType::Bottom, index: 0, price: low, open_time: 0, close_time: 0 },
                end: Fractal { fx_type: FractalType::Top, index: 5, price: high, open_time: 5, close_time: 5 },
            }],
        }
    }

    #[test]
    fn test_basic_center() {
        // 三段重叠
        let segs = vec![seg(0, Direction::Up, 110.0, 100.0), seg(1, Direction::Down, 108.0, 95.0), seg(2, Direction::Up, 112.0, 97.0)];
        let centers = build_centers(&segs);
        assert_eq!(centers.len(), 1);
        assert_eq!(centers[0].zg, 108.0); // min of highs
        assert_eq!(centers[0].zd, 100.0); // max of lows
    }

    #[test]
    fn test_no_overlap() {
        // 无重叠
        let segs = vec![seg(0, Direction::Up, 110.0, 100.0), seg(1, Direction::Down, 95.0, 80.0), seg(2, Direction::Up, 75.0, 60.0)];
        let centers = build_centers(&segs);
        assert_eq!(centers.len(), 0);
    }

    #[test]
    fn test_center_extension() {
        // 第4段仍在中枢区间内，应扩展
        let segs = vec![
            seg(0, Direction::Up, 110.0, 100.0),
            seg(1, Direction::Down, 108.0, 95.0),
            seg(2, Direction::Up, 112.0, 97.0),
            seg(3, Direction::Down, 107.0, 101.0), // 在 zg=108, zd=100 区间内
        ];
        let centers = build_centers(&segs);
        assert_eq!(centers.len(), 1);
        assert_eq!(centers[0].segments.len(), 4);
    }
}
