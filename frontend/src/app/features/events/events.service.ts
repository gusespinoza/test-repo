import { HttpClient, HttpParams } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { Observable } from 'rxjs';

import { Event, EventInput, EventPage } from './events.models';

@Injectable({ providedIn: 'root' })
export class EventsService {
  private readonly http = inject(HttpClient);
  private readonly endpoint = '/api/events';

  listEvents(page = 1, limit = 20): Observable<EventPage> {
    const params = new HttpParams().set('page', page).set('limit', limit);
    return this.http.get<EventPage>(this.endpoint, { params });
  }

  getEvent(id: string): Observable<Event> {
    return this.http.get<Event>(`${this.endpoint}/${encodeURIComponent(id)}`);
  }

  createEvent(input: EventInput): Observable<Event> {
    return this.http.post<Event>(this.endpoint, input);
  }

  updateEvent(id: string, input: EventInput): Observable<Event> {
    return this.http.put<Event>(`${this.endpoint}/${encodeURIComponent(id)}`, input);
  }
}
